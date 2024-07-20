package main

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
	"github.com/gocarina/gocsv"
	"github.com/joho/godotenv"
)

const rootDir = "channels"

type user struct {
	Name     string    `csv:"name"`
	LastSeen time.Time `csv:"last_seen"`
}

func getUsers(client *twitch.Client, channelName string) []string {
	userNames, err := client.Userlist(channelName)
	if err != nil {
		log.Printf("Error getting User list of %s", channelName)
	}
	return userNames
}

func updateUsersFile(channelName string, userNames []string) {
	dirPath := filepath.Join(rootDir, channelName)
	mkDir(dirPath)
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
			if slices.Contains(userNames, user.Name) { continue }
			users = append(users, user)
		}
	}

	timeNow := time.Now()
	for _, userName := range userNames {
		users = append(users, user{userName, timeNow})
	}

	slices.SortFunc(users, func(a, b user) int {
		return a.LastSeen.Compare(b.LastSeen)
	})
	
	if _, err = f.Seek(0,0); err != nil {
		log.Fatal(err)
	}
	if err = gocsv.MarshalFile(&users, f); err != nil {
		log.Fatal(err)
	}
	log.Printf("Updated user list for %s", channelName)
}

func updateUsers(client *twitch.Client, channelName string) {
	for {
		time.Sleep(5 * time.Minute)
		userNames := getUsers(client, channelName)
		updateUsersFile(channelName, userNames)
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

	// client.OnPrivateMessage(func(message twitch.PrivateMessage) {
	// 	client.Say(message.Channel, "hey")
	// })

	channels := strings.Split(os.Getenv("CHANNELS"), ",")
	client.Join(channels...)

	err = client.Connect()
	if err != nil {
		log.Fatal("Error connecting to Twitch")
	}
}
