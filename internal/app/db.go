package app

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

const dbPath = "db.sqlite3"

func OpenDB() *sql.DB {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS channels (
		id INTEGER PRIMARY KEY, login TEXT, access_token TEXT, refresh_token TEXT
	);`)
	if err != nil {
		log.Fatal(err)
	}

	return db
}

func doesChannelExist(db *sql.DB, id string) (bool, error) {
	var exists bool
	query := "SELECT EXISTS (SELECT 1 FROM channels WHERE id = ?)"
	err := db.QueryRow(query, id).Scan(&exists)
	return exists, err
}
