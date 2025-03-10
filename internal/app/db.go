package app

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

const dbPath = "data/db.sqlite3"

func openDB() *sql.DB {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY, login TEXT, access_token TEXT, refresh_token TEXT
	);`)
	if err != nil {
		log.Fatal(err)
	}

	return db
}
