package app

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/antlu/stream-assistant/internal/twitch"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nicklaw5/helix/v2"
)

type channelVip struct {
	ChannelName     string
	Username        string
	LastSeen        sql.NullString
	LastMessageSent sql.NullString
}

type upsertStrategy int

const (
	upsertNothing upsertStrategy = iota
	upsertUpdate
)

type upsertParams struct {
	strategy upsertStrategy
	colVal   map[string]string
}

func newUpsertParams(strategy upsertStrategy, colVal map[string]string) (upsertParams, error) {
	if strategy == upsertUpdate && colVal == nil {
		return upsertParams{}, errors.New("no columns and values provided for update")
	}

	return upsertParams{strategy, colVal}, nil
}

func (up upsertParams) upsertClause() string {
	clause := "ON CONFLICT DO "

	switch up.strategy {
	case upsertUpdate:
		colValPairs := make([]string, 0, len(up.colVal))
		for k, v := range up.colVal {
			colValPairs = append(colValPairs, fmt.Sprintf("%s = %s", k, v))
		}
		clause += fmt.Sprintf("UPDATE SET %s", strings.Join(colValPairs, ","))
	case upsertNothing:
		fallthrough
	default:
		clause += "NOTHING"
	}
	return clause
}

type database struct {
	*sql.DB
}

const dbPath = "db.sqlite3"

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
			refresh_token TEXT NOT NULL,
			synced_at TEXT
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

func (db *database) Begin() (*transaction, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &transaction{tx}, nil
}

func (db *database) WriteInitialData(channelId string, apiClient *twitch.APIClient) (bool, error) {
	exists, err := db.recordExists("channel_viewers", "channel_id", channelId)
	if err != nil || exists {
		return false, err
	}

	channelVips, err := apiClient.GetChannelVips(channelId)
	if err != nil || len(channelVips) == 0 {
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

	upsert, err := newUpsertParams(upsertNothing, nil)
	if err != nil {
		return false, err
	}

	if err := tx.bulkInsert("viewers", []string{"id", "login", "username"}, viewersValues, upsert); err != nil {
		return false, err
	}

	if err := tx.bulkInsert("channel_viewers", []string{"channel_id", "viewer_id"}, chanViewersValues, upsert); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	log.Printf("Wrote initial data for %s", channelId)
	return true, nil
}

func (db *database) UpdatePresenceData(channelId string, onlineVips, offlineVips []helix.ChannelVips) error {
	if len(onlineVips) == 0 && len(offlineVips) == 0 {
		return nil
	}

	var (
		viewersValues, chanOfflineViewersValues, chanOnlineViewersValues [][]any
		viewerIds                                                        []string
	)

	timeNow := time.Now().UTC().Format(time.RFC3339)

	for _, vip := range offlineVips {
		viewersValues = append(viewersValues, []any{vip.UserID, vip.UserLogin, vip.UserName})
		chanOfflineViewersValues = append(chanOfflineViewersValues, []any{channelId, vip.UserID, timeNow})
		viewerIds = append(viewerIds, vip.UserID)
	}

	for _, vip := range onlineVips {
		viewersValues = append(viewersValues, []any{vip.UserID, vip.UserLogin, vip.UserName})
		chanOnlineViewersValues = append(chanOnlineViewersValues, []any{channelId, vip.UserID, timeNow})
		viewerIds = append(viewerIds, vip.UserID)
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(viewerIds)), ",")
	query := fmt.Sprintf("DELETE FROM viewers WHERE id NOT IN (%s)", placeholders)
	if _, err := db.Exec(query, toSliceOfAny(viewerIds)...); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	upsertNothingParams, err := newUpsertParams(upsertNothing, nil)
	if err != nil {
		return err
	}

	if err := tx.bulkInsert("viewers", []string{"id", "login", "username"}, viewersValues, upsertNothingParams); err != nil {
		return err
	}
	if err := tx.bulkInsert("channel_viewers", []string{"channel_id", "viewer_id", "last_seen"}, chanOfflineViewersValues, upsertNothingParams); err != nil {
		return err
	}

	upsertUpdateParams, err := newUpsertParams(upsertUpdate, map[string]string{"last_seen": "excluded.last_seen"})
	if err != nil {
		return err
	}

	if err := tx.bulkInsert("channel_viewers", []string{"channel_id", "viewer_id"}, chanOnlineViewersValues, upsertUpdateParams); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Printf("Updated viewers for %s", channelId)
	return nil
}

func (db *database) recordExists(tableName, columnName, value string) (bool, error) {
	var exists bool
	query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE %s = ?)", tableName, columnName)
	err := db.QueryRow(query, value).Scan(&exists)
	return exists, err
}

type transaction struct {
	*sql.Tx
}

func (tx *transaction) bulkInsert(tableName string, columns []string, valGroups [][]any, upsertParams upsertParams) error {
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
		"INSERT INTO %s (%s) VALUES %s %s",
		tableName,
		strings.Join(columns, ","),
		strings.Join(placeholders, ","),
		upsertParams.upsertClause(),
	)

	_, err := tx.Exec(query, args...)

	return err
}

func toSliceOfAny[T any](slice []T) []any {
	result := make([]any, len(slice))
	for i, v := range slice {
		result[i] = v
	}
	return result
}
