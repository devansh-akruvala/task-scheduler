package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/devansh-akruvala/task-scheduler/pkg/common"

	"github.com/jackc/pgx/pgtype"
	"github.com/jackc/pgx/v4/pgxpool"
)

type CommandRequest struct {
	Command     string `json:"command"`
	ScheduledAt string `json:"scheduled_at"`
}

type Task struct {
	Id          string
	Command     string
	ScheduledAt pgtype.Timestamp
	PickedAt    pgtype.Timestamp
	StartedAt   pgtype.Timestamp
	CompletedAt pgtype.Timestamp
	FailedAt    pgtype.Timestamp
}

// SchedulerServer represents an HTTP server that manages tasks.
type SchedulerServer struct {
	serverPort         string
	dbConnectionString string
	dbPool             *pgxpool.Pool
	ctx                context.Context
	cancel             context.CancelFunc
	httpServer         *http.Server
}

func NewServer(port string, dbConnectionString string) *SchedulerServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &SchedulerServer{
		serverPort:         port,
		dbConnectionString: dbConnectionString,
		ctx:                ctx,
		cancel:             cancel,
	}
}

func (s *SchedulerServer) Start() error {
	var err error
	s.dbPool, err = common.ConnectToDatabase(s.ctx, s.dbConnectionString)
	if err != nil {
		return err
	}

	http.HandleFunc("/schedule", s.handleScheduleTask)
	http.HandleFunc("/status/", s.handleGetTaskStatus)

	s.httpServer = &http.Server{
		Addr: s.serverPort,
	}

	log.Printf("Starting Scheduler server on %s\n", s.serverPort)

	// Start the server in a separate goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %s\n", err)
		}
	}()

	// Return awaitShutdown
	return s.awaitShutdown()
}

// handleScheduleTask handles POST requests to add new tasks.
func (s *SchedulerServer) handleScheduleTask(w http.ResponseWriter, r *http.Request) {

	if r.Method != "POST" {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode the JSON body
	var commandReq CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&commandReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("Received schedule request: %+v", commandReq)

	// Parse the scheduled_at time
	scheduledTime, err := time.Parse(time.RFC3339, commandReq.ScheduledAt)
	if err != nil {
		http.Error(w, "Invalid date format. Use ISO 8601 format.", http.StatusBadRequest)
		return
	}

	// Convert the scheduled time to Unix timestamp
	unixTimestamp := time.Unix(scheduledTime.Unix(), 0)

	taskId, err := s.insertTaskIntoDB(context.Background(), Task{Command: commandReq.Command, ScheduledAt: pgtype.Timestamp{Time: unixTimestamp}})

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to submit task. Error: %s", err.Error()),
			http.StatusInternalServerError)
		return
	}

	// Respond with the parsed data (for demonstration purposes)
	response := struct {
		Command     string `json:"command"`
		ScheduledAt int64  `json:"scheduled_at"`
		TaskID      string `json:"task_id"`
	}{
		Command:     commandReq.Command,
		ScheduledAt: unixTimestamp.Unix(),
		TaskID:      taskId,
	}

	jsonResponse, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}

func (s *SchedulerServer) handleGetTaskStatus(w http.ResponseWriter, r *http.Request) {

	if r.Method != "GET" {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// get task id from query params
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		http.Error(w, "Task Id is Required", http.StatusBadRequest)
		return
	}

	// query db to get task status
	var task Task
	err := s.dbPool.QueryRow(context.Background(), "SELECT * FROM tasks WHERE id = $1", taskID).Scan(&task.Id, &task.Command, &task.ScheduledAt, &task.PickedAt, &task.StartedAt, &task.CompletedAt, &task.FailedAt)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get task status: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	// preparing response
	response := struct {
		TaskID      string `json:"task_id"`
		Command     string `json:"command"`
		ScheduledAt string `json:"scheduled_at,omitempty"`
		PickedAt    string `json:"picked_at,omitempty"`
		StartedAt   string `json:"started_at,omitempty"`
		CompletedAt string `json:"completed_at,omitempty"`
		FailedAt    string `json:"failed_at,omitempty"`
	}{
		TaskID:      task.Id,
		Command:     task.Command,
		ScheduledAt: "",
		PickedAt:    "",
		StartedAt:   "",
		CompletedAt: "",
		FailedAt:    "",
	}

	if task.ScheduledAt.Status == 2 {
		response.ScheduledAt = task.ScheduledAt.Time.String()
	}

	if task.PickedAt.Status == 2 {
		response.PickedAt = task.PickedAt.Time.String()
	}

	if task.StartedAt.Status == 2 {
		response.PickedAt = task.StartedAt.Time.String()
	}

	if task.CompletedAt.Status == 2 {
		response.PickedAt = task.CompletedAt.Time.String()
	}

	if task.FailedAt.Status == 2 {
		response.PickedAt = task.FailedAt.Time.String()
	}

	jsonResponse, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "failed to convert response to json", http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "application/json")

	w.Write(jsonResponse)

}

func (s *SchedulerServer) insertTaskIntoDB(ctx context.Context, task Task) (string, error) {
	sqlStatement := "INSERT INTO tasks (command, scheduled_at) VALUES ($1,$2) RETURNING id"

	var insertedID string

	err := s.dbPool.QueryRow(ctx, sqlStatement, task.Command, task.ScheduledAt.Time).Scan(&insertedID)

	if err != nil {
		log.Fatal("Failed to Insert task in DB")
		return "", err
	}

	return insertedID, nil
}

func (s *SchedulerServer) awaitShutdown() error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	return s.Stop()
}

// Stop gracefully shuts down the SchedulerServer and the database connection pool.
func (s *SchedulerServer) Stop() error {
	s.dbPool.Close()
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}
	log.Println("Scheduler server and database pool stopped")
	return nil
}
