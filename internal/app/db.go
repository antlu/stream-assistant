package app

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/antlu/stream-assistant/internal/twitch"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nicklaw5/helix/v2"
)

const dbPath = "db.sqlite3"

type database struct {
	*sql.DB
}

func OpenDB() *database {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	wrapper := database{DB: db}

	_, err = wrapper.Exec(`
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

	return &wrapper
}

func (db database) WriteInitialData(channelId string, apiClient *twitch.ApiClient) (bool, error) {
	exists, err := db.recordExists("channel_viewers", "channel_id", channelId)
	if err != nil || exists {
		return false, err
	}

	channelVips, err := apiClient.GetChannelVips(channelId)
	if err != nil {
		return false, err
	}

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	viewersValues := make([][]any, len(channelVips))
	chanViewersValues := make([][]any, len(channelVips))

	for i, vip := range channelVips {
		viewersValues[i] = []any{vip.UserID, vip.UserLogin, vip.UserName}
		chanViewersValues[i] = []any{channelId, vip.UserID}
	}

	if err := db.bulkInsert("viewers", []string{"id", "login", "username"}, viewersValues); err != nil {
		return false, err
	}

	if err := db.bulkInsert("channel_viewers", []string{"channel_id", "viewer_id"}, chanViewersValues); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	log.Printf("Wrote initial data for %s", channelId)
	return true, nil
}

func (db database) UpdatePresenceData(channelId string, onlineVips, offlineVips []helix.ChannelVips) error {
	var viewersValues, chanViewersValues [][]any
	var dbViewerIds []string

	rows, err := db.Query("SELECT id FROM viewers")
	if err != nil {
		return err
	}
	for rows.Next() {
		var viewerId string
		if err := rows.Scan(&viewerId); err != nil {
			return err
		}
		dbViewerIds = append(dbViewerIds, viewerId)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	timeNow := time.Now().UTC().Format(time.RFC3339)

	for _, vip := range offlineVips {
		if slices.Contains(dbViewerIds, vip.UserID) {
			continue
		}

		viewersValues = append(viewersValues, []any{vip.UserID, vip.UserLogin, vip.UserName})
		chanViewersValues = append(chanViewersValues, []any{channelId, vip.UserID, timeNow})
	}

	for _, vip := range onlineVips {
		if slices.Contains(dbViewerIds, vip.UserID) {
			query := "UPDATE channel_viewers SET last_seen = ? WHERE channel_id = ? AND viewer_id = ?"
			_, err := db.Exec(query, timeNow, channelId, vip.UserID)
			if err != nil {
				return err
			}
		} else {
			viewersValues = append(viewersValues, []any{vip.UserID, vip.UserLogin, vip.UserName})
			chanViewersValues = append(chanViewersValues, []any{channelId, vip.UserID, timeNow})
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.bulkInsert("viewers", []string{"id", "login", "username"}, viewersValues); err != nil {
		return err
	}
	if err := db.bulkInsert("channel_viewers", []string{"channel_id", "viewer_id", "last_seen"}, chanViewersValues); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// delete from viewers where id not in the list of ids from onlineVips and offlineVips

	return nil
}

func (db database) recordExists(tableName, columnName, value string) (bool, error) {
	var exists bool
	query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE %s = ?)", tableName, columnName)
	err := db.QueryRow(query, value).Scan(&exists)
	return exists, err
}

func (db database) bulkInsert(tableName string, columns []string, valGroups [][]any) error {
	if len(valGroups) == 0 {
		return nil
	}

	var (
		placeholders []string
		args         []any
	)

	for _, valGroup := range valGroups {
		if len(valGroup) != len(columns) {
			return errors.New("values count doesn't match columns count")
		}

		placeholders = append(placeholders, "("+strings.TrimRight(strings.Repeat("?,", len(columns)), ",")+")")
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
