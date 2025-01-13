package database

import (
	"database/sql"
	_ "github.com/lib/pq"
)

func ConnectDB() (*sql.DB, error) {
	connStr := "user=postgres password=admin dbname=gradedchalp32 sslmode=disable"
	return sql.Open("postgres", connStr)
}
