package main

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/nicklaw5/helix/v2"
	"github.com/gempir/go-twitch-irc/v4"
	"github.com/joho/godotenv"

	"github.com/antlu/stream-assistant/internal"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	nick := os.Getenv("SA_NICK")
	pass := os.Getenv("SA_PASS")
	channelUatPairs := strings.Split(os.Getenv("SA_CHANNEL_UAT_PAIRS"), ",")

	apiClient, err := helix.NewClient(&helix.Options{
		ClientID:        "jmaoofuyr1c4v8lqzdejzfppdj5zym",
		UserAccessToken: os.Getenv("SA_USER_ACCESS_TOKEN"),
	})
	if err != nil {
		log.Fatal("Error creating API client")
	}

	channelNames, channels := internal.PrepareChannels(channelUatPairs, apiClient)

	ircClient := twitch.NewClient(nick, pass)
	ircClient.Capabilities = append(ircClient.Capabilities, twitch.MembershipCapability)

	ircClient.OnSelfJoinMessage(func(message twitch.UserJoinMessage) {
		go func() {
			channelName := message.Channel
			log.Printf("Joined %s", channelName)
			apiClient := internal.NewApiClient(channelName, channels[channelName].Name)

			for {
				time.Sleep(5 * time.Minute)
				if channels[channelName].IsLive {
					internal.UpdateUsers(ircClient, apiClient, channelName)
				}
			}
		}()
	})

	// ircClient.OnPrivateMessage(func(message twitch.PrivateMessage) {
	// 	ircClient.Say(message.Channel, "hey")
	// })

	ircClient.Join(channelNames...)

	internal.StartTwitchWSCommunication(apiClient, channels)

	err = ircClient.Connect()
	if err != nil {
		log.Fatal("Error connecting to Twitch")
	}
}
