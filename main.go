package main

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
	"github.com/joho/godotenv"
)

const rootDir = "channels"

func getUsers(client *twitch.Client, channelName string) []string {
	userNames, err := client.Userlist(channelName)
	if err != nil {
		log.Printf("Error getting User list of %s", channelName)
	}
	return userNames
}

func writeUsers(channelName string, userNames []string) {
	dirPath := filepath.Join(rootDir, channelName)
	mkDir(dirPath)
	f, err := os.OpenFile(filepath.Join(dirPath, "users.csv"), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	f.WriteString(userNames[0])
}

func updateUsers(client *twitch.Client, channelName string) {
	for {
		time.Sleep(5 * time.Minute)
		userNames := getUsers(client, channelName)
		writeUsers(channelName, userNames)
	}
}

func mkDir(path string) {
	err := os.Mkdir(path, os.ModePerm)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		log.Fatalf("Error creating %s directory", path)
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	mkDir(rootDir)

	nick := os.Getenv("NICK")
	pass := os.Getenv("PASS")

	client := twitch.NewClient(nick, pass)

	client.OnSelfJoinMessage(func(message twitch.UserJoinMessage) {
		go func() {
			channelName := message.Channel
			log.Printf("Joined %s", channelName)
			updateUsers(client, channelName)
		}()
	})

	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		client.Say(message.Channel, "hey")
	})

	channels := strings.Split(os.Getenv("CHANNELS"), ",")
	client.Join(channels...)

	err = client.Connect()
	if err != nil {
		log.Fatal("Error connecting to Twitch")
	}
}
