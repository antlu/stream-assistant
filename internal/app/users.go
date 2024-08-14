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

func convertUsernamesToUsers(userNames []string, users *[]types.User) {
	timeNow := time.Now()
	for _, userName := range userNames {
		*users = append(*users, types.User{Name: userName, LastSeen: timeNow})
	}
}

func WriteDataToUsersFileIfNotExists(channelName string, callback func(string) ([]string, error)) {
	f, closer, err := createUsersFileIfNotExists(channelName)
	if err != nil {
		return
	}
	defer closer()

	userNames, err := callback(channelName)
	if err != nil {
		log.Fatal(err)
	}

	users := make([]types.User, 0, len(userNames))
	convertUsernamesToUsers(userNames, &users)

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

func UpdateUsersFile(channelName string, userNames []string) {
	f, err := os.OpenFile(filePath(channelName), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		log.Fatal(err)
	}

	users := []types.User{}
	if fi.Size() != 0 {
		usersFromFile := []types.User{}
		if err = gocsv.UnmarshalFile(f, &usersFromFile); err != nil {
			log.Fatal(err)
		}
		// filter online users
		for _, user := range usersFromFile {
			if slices.Contains(userNames, user.Name) {
				continue
			}
			users = append(users, user)
		}
	}

	convertUsernamesToUsers(userNames, &users)

	if _, err = f.Seek(0, 0); err != nil {
		log.Fatal(err)
	}
	if err = gocsv.MarshalFile(&users, f); err != nil {
		log.Fatal(err)
	}

	log.Printf("Updated users list for %s", channelName)
}

func GetOnlineVips(ircClient *twitchIRC.Client, apiClient *twitch.ApiClient, channelName string) ([]string, error) {
	userNames, err := ircClient.Userlist(channelName)
	if err != nil {
		return nil, err
	}

	vipNames, err := apiClient.GetVipNames(channelName)
	if err != nil {
		return nil, err
	}

	presentVipNames := make([]string, 0, len(vipNames))
	for _, vipName := range vipNames {
		if slices.Contains(userNames, vipName) {
			presentVipNames = append(presentVipNames, vipName)
		}
	}

	return presentVipNames, nil
}
