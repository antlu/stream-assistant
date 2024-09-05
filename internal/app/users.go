package app

import (
	"bufio"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	twitchIRC "github.com/gempir/go-twitch-irc/v4"
	"github.com/gocarina/gocsv"

	"github.com/antlu/stream-assistant/internal"
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

func appendUsersAsIs(usersFromFile []types.User, userNames []string, users *[]types.User) {
	for _, user := range usersFromFile {
		if slices.Contains(userNames, user.Name) {
			*users = append(*users, user)
		}
	}
}

func appendUsersUpdated(userNames []string, users *[]types.User) {
	timeNow := time.Now()
	for _, userName := range userNames {
		*users = append(*users, types.User{Name: userName, LastSeen: timeNow})
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

	users := make([]types.User, 0, len(userNames))
	appendUsersUpdated(userNames, &users)

	if err = gocsv.MarshalFile(&users, f); err != nil {
		log.Fatal(err)
	}

	log.Printf("Wrote initial data for %s", channelName)
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

	users := []types.User{}
	usersFromFile := []types.User{}
	if err = gocsv.UnmarshalFile(f, &usersFromFile); err != nil {
		log.Fatal(err)
	}

	appendUsersAsIs(usersFromFile, offlineUserNames, &users)
	appendUsersUpdated(onlineUserNames, &users)

	if _, err = f.Seek(0, 0); err != nil {
		log.Fatal(err)
	}
	if err = gocsv.MarshalFile(&users, f); err != nil {
		log.Fatal(err)
	}

	log.Printf("Updated users list for %s", channelName)
}

func GetOnlineOfflineVips(ircClient *twitchIRC.Client, apiClient *twitch.ApiClient, channelName string) ([]string, []string, error) {
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
