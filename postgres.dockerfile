FROM postgres:17.1

WORKDIR /app

COPY pkg/db/setup.sql /docker-entrypoint-initdb.d/
