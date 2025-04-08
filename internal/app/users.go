package app

import (
	"bufio"
	"database/sql"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gocarina/gocsv"

	"github.com/antlu/stream-assistant/internal/twitch"
)

const (
	dataDirName   = "data"
	usersFileName = "users.csv"
)

func filePath(channelName string) string {
	return filepath.Join(dataDirName, channelName, usersFileName)
}

func createUsersFileIfNotExists(channelName string) (*os.File, func(), error) {
	filePath := filePath(channelName)

	_, err := os.Stat(filePath)
	if err == nil {
		return nil, nil, os.ErrExist
	}

	if !errors.Is(err, os.ErrNotExist) {
		log.Fatal(err)
	}

	dirPath := filepath.Join(dataDirName, channelName)

	err = os.MkdirAll(dirPath, os.ModePerm)
	if err != nil {
		log.Fatalf("Error creating %s directory", dirPath)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return nil, nil, err
	}

	return f, func() {
		f.Close()
	}, nil
}

func appendUsersAsIs(usersFromFile []User, userNames []string, users *[]User) {
	for _, user := range usersFromFile {
		if slices.Contains(userNames, user.Name) {
			*users = append(*users, user)
		}
	}
}

func appendUsersUpdated(userNames []string, users *[]User) {
	timeNow := time.Now()
	for _, userName := range userNames {
		*users = append(*users, User{Name: userName, LastSeen: timeNow})
	}
}

func WriteInitialDataToUsersFile(channelName string, apiClient *twitch.ApiClient) {
	f, close, err := createUsersFileIfNotExists(channelName)
	if err != nil {
		return
	}
	defer close()

	usersFromResponse, err := apiClient.GetVips(channelName)
	if err != nil {
		log.Fatal(err)
	}

	userNames := make([]string, 0, len(usersFromResponse))
	for _, user := range usersFromResponse {
		userNames = append(userNames, user.UserLogin)
	}

	users := make([]User, 0, len(userNames))
	appendUsersUpdated(userNames, &users)

	if err = gocsv.MarshalFile(&users, f); err != nil {
		log.Fatal(err)
	}

	log.Printf("Wrote initial data for %s", channelName)
}

func WriteInitialData(db *sql.DB, channelId string, apiClient *twitch.ApiClient) (bool, error) {
	exists, err := recordExists(db, "channel_viewers", "channel_id", channelId)
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

	for i, channelVip := range channelVips {
		viewersValues[i] = []any{channelVip.UserID, channelVip.UserLogin, channelVip.UserName}
		chanViewersValues[i] = []any{channelId, channelVip.UserID}
	}

	err = bulkInsert(db, "viewers", []string{"id", "login", "username"}, viewersValues)
	if err != nil {
		return false, err
	}

	err = bulkInsert(db, "channel_viewers", []string{"channel_id", "viewer_id"}, chanViewersValues)
	if err != nil {
		return false, err
	}

	err = tx.Commit()
	if err != nil {
		return false, err
	}

	log.Printf("Wrote initial data for %s", channelId)
	return true, nil
}

func GetFirstUserFromFile(channelName string) string {
	f, err := os.Open(filePath(channelName))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan()
	scanner.Scan()
	userName := strings.Split(scanner.Text(), ",")[0]

	return userName
}

func UpdateUsersFile(channelName string, onlineUserNames, offlineUserNames []string) {
	f, err := os.OpenFile(filePath(channelName), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	users := []User{}
	usersFromFile := []User{}
	if err = gocsv.UnmarshalFile(f, &usersFromFile); err != nil {
		log.Fatal(err)
	}

	appendUsersAsIs(usersFromFile, offlineUserNames, &users)
	appendUsersUpdated(onlineUserNames, &users)

	if _, err = f.Seek(0, io.SeekStart); err != nil {
		log.Fatal(err)
	}
	if err = gocsv.MarshalFile(&users, f); err != nil {
		log.Fatal(err)
	}

	log.Printf("Updated users list for %s", channelName)
}

func appendUserToFile(channelName string, userName string) {
	f, err := os.OpenFile(filePath(channelName), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	users := []User{{Name: userName, LastSeen: time.Now()}}
	if err = gocsv.MarshalWithoutHeaders(&users, f); err != nil {
		log.Fatal(err)
	}
}

func GetOnlineOfflineVips(ircClient *twitch.IRCClient, apiClient *twitch.ApiClient, channelName string) ([]string, []string, error) {
	userLogins, err := ircClient.Userlist(channelName)
	if err != nil {
		return nil, nil, err
	}

	vips, err := apiClient.GetVips(channelName)
	if err != nil {
		return nil, nil, err
	}

	presentVipLogins := make([]string, 0, len(vips))
	absentVipLogins := make([]string, 0, len(vips))
	for _, vip := range vips {
		if slices.Contains(userLogins, vip.UserLogin) {
			presentVipLogins = append(presentVipLogins, vip.UserLogin)
		} else {
			absentVipLogins = append(absentVipLogins, vip.UserLogin)
		}
	}

	return presentVipLogins, absentVipLogins, nil
}
