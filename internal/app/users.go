package app

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	twitchIRC "github.com/gempir/go-twitch-irc/v4"
	"github.com/gocarina/gocsv"

	"github.com/antlu/stream-assistant/internal"
	"github.com/antlu/stream-assistant/internal/twitch"
)

func updateUsersFile(channelName string, userNames []string) {
	dirPath := filepath.Join("data", channelName)

	err := os.MkdirAll(dirPath, os.ModePerm)
	if err != nil {
		log.Fatalf("Error creating %s directory", dirPath)
	}

	f, err := os.OpenFile(filepath.Join(dirPath, "users.csv"), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		log.Fatal(err)
	}

	users := []structs.User{}
	if fi.Size() != 0 {
		usersFromFile := []structs.User{}
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

	timeNow := time.Now()
	for _, userName := range userNames {
		users = append(users, structs.User{Name: userName, LastSeen: timeNow})
	}

	if _, err = f.Seek(0, 0); err != nil {
		log.Fatal(err)
	}
	if err = gocsv.MarshalFile(&users, f); err != nil {
		log.Fatal(err)
	}
	log.Printf("Updated user list for %s", channelName)
}

func UpdateUsers(ircClient *twitchIRC.Client, apiClient *twitch.ApiClient, channelName string) {
	userNames, err := ircClient.Userlist(channelName)
	if err != nil {
		log.Print(err)
		return
	}

	vipNames, err := apiClient.GetVipNames(channelName)
	if err != nil {
		log.Print(err)
		return
	}

	presentVipNames := make([]string, 0, len(vipNames))
	for _, vipName := range vipNames {
		if slices.Contains(userNames, vipName) {
			presentVipNames = append(presentVipNames, vipName)
		}
	}

	updateUsersFile(channelName, presentVipNames)
}
