package main

import (
	"log"
	"os"
	"strings"

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
			apiClient := internal.NewApiClient(channelName, channelUatMap[channelName])
			internal.UpdateUsers(ircClient, apiClient, channelName)
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
