package main

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/nicklaw5/helix/v2"
	twitchIRC "github.com/gempir/go-twitch-irc/v4"
	"github.com/joho/godotenv"

	"github.com/antlu/stream-assistant/internal/twitch"
	"github.com/antlu/stream-assistant/internal/app"
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

	channels := app.PrepareChannels(channelUatPairs, apiClient)

	ircClient := twitchIRC.NewClient(nick, pass)
	ircClient.Capabilities = append(ircClient.Capabilities, twitchIRC.MembershipCapability)

	ircClient.OnSelfJoinMessage(func(message twitchIRC.UserJoinMessage) {
		go func() {
			channelName := message.Channel
			log.Printf("Joined %s", channelName)
			apiClient := twitch.NewApiClient(channelName, channels.Dict[channelName].UAT)

			for {
				time.Sleep(5 * time.Minute)
				if channels.Dict[channelName].IsLive {
					app.UpdateUsers(ircClient, apiClient, channelName)
				}
			}
		}()
	})

	// ircClient.OnPrivateMessage(func(message twitchIRC.PrivateMessage) {
	// 	ircClient.Say(message.Channel, "hey")
	// log.Printf("%s: %s", message.User.Name, message.Message)
	// })

	ircClient.Join(channels.Names...)

	app.StartTwitchWSCommunication(apiClient, channels)

	err = ircClient.Connect()
	if err != nil {
		log.Fatal("Error connecting to Twitch")
	}
}
