package internal

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nicklaw5/helix/v2"
	"github.com/gempir/go-twitch-irc/v4"
	"github.com/gocarina/gocsv"
)

type user struct {
	Name     string    `csv:"name"`
	LastSeen time.Time `csv:"last_seen"`
}

type channel struct {
	ID     string
	Name   string
	UAT    string
	IsLive bool
}

type channelsDict map[string]*channel

func PrepareChannels(channelUatPairs []string, apiClient *helix.Client) ([]string, channelsDict) {
	numberOfChannels := len(channelUatPairs)
	channels := make(channelsDict, numberOfChannels)
	channelNames := make([]string, 0, numberOfChannels)
	for _, pair := range channelUatPairs {
		parts := strings.Split(pair, ":")
		channels[parts[0]] = &channel{Name: parts[0], UAT: parts[1]}
		channelNames = append(channelNames, parts[0])
	}

	// Get channel IDs
	usersResp, err := apiClient.GetUsers(&helix.UsersParams{Logins: channelNames})
	if err != nil {
		log.Fatal("Error getting users info")
	}
	for _, user := range usersResp.Data.Users {
		channels[user.Login].ID = user.ID
	}

	// Get live streams
	streamsResp, err := apiClient.GetStreams(&helix.StreamsParams{UserLogins: channelNames})
	if err != nil {
		log.Fatal("Error getting streams info")
	}
	for _, stream := range streamsResp.Data.Streams {
		channels[stream.UserLogin].IsLive = true
	}

	return channelNames, channels
}

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

	users := []user{}
	if fi.Size() != 0 {
		usersFromFile := []user{}
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
		users = append(users, user{userName, timeNow})
	}

	if _, err = f.Seek(0, 0); err != nil {
		log.Fatal(err)
	}
	if err = gocsv.MarshalFile(&users, f); err != nil {
		log.Fatal(err)
	}
	log.Printf("Updated user list for %s", channelName)
}

func UpdateUsers(ircClient *twitch.Client, apiClient *apiClient, channelName string) {
	userNames, err := ircClient.Userlist(channelName)
	if err != nil {
		log.Print(err)
		return
	}

	vipNames, err := apiClient.getVipNames(channelName)
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
