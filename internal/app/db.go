package app

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const dbPath = "db.sqlite3"

func OpenDB() *sql.DB {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`
		PRAGMA foreign_keys = ON;

		CREATE TABLE IF NOT EXISTS channels (
			id INTEGER PRIMARY KEY,
			login TEXT NOT NULL,
			access_token TEXT NOT NULL,
			refresh_token TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS viewers (
			id INTEGER PRIMARY KEY,
			login TEXT NOT NULL,
			username TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS channel_viewers (
			channel_id INTEGER,
			viewer_id INTEGER,
			last_seen TEXT,
			last_message_sent TEXT,
			PRIMARY KEY (channel_id, viewer_id),
			FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE,
			FOREIGN KEY (viewer_id) REFERENCES viewers(id) ON DELETE CASCADE
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	return db
}

func recordExists(db *sql.DB, tableName, columnName, value string) (bool, error) {
	var exists bool
	query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE %s = ?)", tableName, columnName)
	err := db.QueryRow(query, value).Scan(&exists)
	return exists, err
}

func bulkInsert(db *sql.DB, tableName string, columns []string, valGroups [][]any) error {
	if len(valGroups) == 0 {
		return nil
	}

	var (
		placeholders []string
		args []any
	)

	for _, valGroup := range valGroups {
		if len(valGroup) != len(columns) {
			return errors.New("values count doesn't match columns count")
		}
		
		placeholders = append(placeholders, "(" + strings.TrimRight(strings.Repeat("?,", len(columns)), ",") + ")")
		args = append(args, valGroup...)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES %s",
		tableName,
		strings.Join(columns, ","),
		strings.Join(placeholders, ","),
	)

	_, err := db.Exec(query, args...)

	return err
}
