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

type apiClient struct {
	*helix.Client
	channel string
}

func newApiClient(channel, uat string) *apiClient {
	client, err := helix.NewClient(&helix.Options{
		ClientID:        "jmaoofuyr1c4v8lqzdejzfppdj5zym",
		UserAccessToken: uat,
	})
	if err != nil {
		log.Fatalf("Error creating API client for %s", channel)
	}

	return &apiClient{client, channel}
}

type user struct {
	Name     string    `csv:"name"`
	LastSeen time.Time `csv:"last_seen"`
}

func (ac apiClient) getUsersInfo(names ...string) ([]helix.User, error) {
	resp, err := ac.GetUsers(&helix.UsersParams{Logins: names})

	if err != nil {
		log.Print("Error getting users info")
		return nil, err
	}

	return resp.Data.Users, nil
}

func (ac apiClient) getVipNames(channelName string) ([]string, error) {
	usersInfo, err := ac.getUsersInfo(channelName)
	if err != nil {
		return nil, err
	}

	resp, err := ac.GetChannelVips(&helix.GetChannelVipsParams{
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

func updateUsers(ircClient *twitch.Client, apiClient *apiClient, channelName string) {
	for {
		time.Sleep(5 * time.Minute)
		userNames, err := ircClient.Userlist(channelName)
		if err != nil {
			log.Print(err)
			continue
		}

		vipNames, err := apiClient.getVipNames(channelName)
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
	channelUatPairs := strings.Split(os.Getenv("SA_CHANNEL_UAT_PAIRS"), ",")

	channelUatMap := make(map[string]string, len(channelUatPairs))
	for _, pair := range channelUatPairs {
		parts := strings.Split(pair, ":")
		channelUatMap[parts[0]] = parts[1]
	}

	ircClient := twitch.NewClient(nick, pass)
	ircClient.Capabilities = append(ircClient.Capabilities, twitch.MembershipCapability)

	ircClient.OnSelfJoinMessage(func(message twitch.UserJoinMessage) {
		go func() {
			channelName := message.Channel
			log.Printf("Joined %s", channelName)
			apiClient := newApiClient(channelName, channelUatMap[channelName])
			updateUsers(ircClient, apiClient, channelName)
		}()
	})

	// ircClient.OnPrivateMessage(func(message twitch.PrivateMessage) {
	// 	ircClient.Say(message.Channel, "hey")
	// })

	channels := make([]string, 0, len(channelUatMap))
	for channel := range channelUatMap {
		channels = append(channels, channel)
	}
	ircClient.Join(channels...)

	err = ircClient.Connect()
	if err != nil {
		log.Fatal("Error connecting to Twitch")
	}
}
