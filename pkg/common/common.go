package common

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

const (
	DefaultHeartbeat = 5 * time.Second
)

func GetDBConnectionString() string {
	var missingEnvVars []string

	checkEnvVars := func(envVar, envVarName string) {
		if envVar == "" {
			missingEnvVars = append(missingEnvVars, envVarName)
		}
	}

	dbUser := os.Getenv("POSTGRES_USER")
	checkEnvVars(dbUser, "POSTGRES_USER")

	dbPassword := os.Getenv("POSTGRES_PASSWORD")
	checkEnvVars(dbPassword, "POSTGRES_PASSWORD")

	dbName := os.Getenv("POSTGRES_DB")
	checkEnvVars(dbName, "POSTGRES_DB")

	dbHost := os.Getenv("POSTGRES_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	if len(missingEnvVars) > 0 {
		log.Fatalf("Env vars are not set : %s", strings.Join(missingEnvVars, ", "))
	}

	return fmt.Sprintf("postgres://%s:%s@%s:5431/%s", dbUser, dbPassword, dbHost, dbName)
}

func ConnectToDatabase(ctx context.Context, dbConnectionString string) (*pgxpool.Pool, error) {
	var dbPool *pgxpool.Pool
	var err error
	retryCounter := 0

	for retryCounter < 5 {
		dbPool, err = pgxpool.Connect(ctx, dbConnectionString)
		if err == nil {
			break
		}
		log.Printf("Failed to connect to db. Retyring in 5 Seconds....")
		time.Sleep(DefaultHeartbeat)
		retryCounter++
	}
	if err != nil {
		log.Printf("Ran out of retries to connect to database (5)")
		return nil, err
	}
	log.Printf("Connected to database")
	return dbPool, nil
}
