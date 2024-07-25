package main

import (
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
	"github.com/gocarina/gocsv"
	"github.com/joho/godotenv"
	"github.com/nicklaw5/helix/v2"
)

const rootDir = "channels"

var apiClient *helix.Client

type user struct {
	Name     string    `csv:"name"`
	LastSeen time.Time `csv:"last_seen"`
}

func getUserNames(client *twitch.Client, channelName string) ([]string, error) {
	userNames, err := client.Userlist(channelName)
	if err != nil {
		log.Printf("Error getting User list of %s", channelName)
		return nil, err
	}
	return userNames, nil
}

func getUsersInfo(names ...string) ([]helix.User, error) {
	resp, err := apiClient.GetUsers(&helix.UsersParams{
		Logins: names,
	})

	if err != nil {
		log.Print("Error getting users info")
		return nil, err
	}

	return resp.Data.Users, nil
}

func getVipNames(channelName string) ([]string, error) {
	usersInfo, err := getUsersInfo(channelName)
	if err != nil {
		return nil, err
	}

	resp, err := apiClient.GetChannelVips(&helix.GetChannelVipsParams{
		BroadcasterID: usersInfo[0].ID,
		First:         100,
	})
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Error getting VIPs of %s", channelName)
		if err == nil {
			err = errors.New(resp.ErrorMessage)
		}
		return nil, err
	}

	vips := make([]string, 0, len(resp.Data.ChannelsVips))
	for _, vip := range resp.Data.ChannelsVips {
		vips = append(vips, vip.UserLogin)
	}

	return vips, nil
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

	slices.SortFunc(users, func(a, b user) int {
		return a.LastSeen.Compare(b.LastSeen)
	})

	if _, err = f.Seek(0, 0); err != nil {
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
		userNames, err := getUserNames(client, channelName)
		if err != nil {
			log.Print(err)
			continue
		}

		vipNames, err := getVipNames(channelName)
		if err != nil {
			log.Print(err)
			continue
		}

		presentVipNames := make([]string, 0, len(vipNames))
		for _, vipName := range vipNames {
			if slices.Contains(userNames, vipName) {
				presentVipNames = append(presentVipNames, vipName)
			}
		}

		updateUsersFile(channelName, presentVipNames)
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

	nick := os.Getenv("SA_NICK")
	pass := os.Getenv("SA_PASS")

	ircClient := twitch.NewClient(nick, pass)
	ircClient.Capabilities = append(ircClient.Capabilities, twitch.MembershipCapability)

	ircClient.OnSelfJoinMessage(func(message twitch.UserJoinMessage) {
		go func() {
			channelName := message.Channel
			log.Printf("Joined %s", channelName)
			updateUsers(ircClient, channelName)
		}()
	})

	// ircClient.OnPrivateMessage(func(message twitch.PrivateMessage) {
	// 	ircClient.Say(message.Channel, "hey")
	// })

	channels := strings.Split(os.Getenv("SA_CHANNELS"), ",")
	ircClient.Join(channels...)

	apiClient, err = helix.NewClient(&helix.Options{
		ClientID:        "jmaoofuyr1c4v8lqzdejzfppdj5zym",
		UserAccessToken: os.Getenv("SA_USER_ACCESS_TOKEN"),
	})
	if err != nil {
		log.Fatal("Error creating API client")
	}

	err = ircClient.Connect()
	if err != nil {
		log.Fatal("Error connecting to Twitch")
	}
}
