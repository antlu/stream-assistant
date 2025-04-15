package main

import (
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	twitchIRC "github.com/gempir/go-twitch-irc/v4"
	"github.com/joho/godotenv"

	"github.com/antlu/stream-assistant/internal/app"
	"github.com/antlu/stream-assistant/internal/crypto"
	"github.com/antlu/stream-assistant/internal/twitch"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	db := app.OpenDB()
	defer db.Close()

	tokenManager := twitch.NewTokenManager(db, crypto.Cipher(os.Getenv("SA_SECURE_KEY")))

	botName := os.Getenv("SA_BOT_NAME")

	apiClient, err := twitch.NewApiClient(botName, tokenManager)
	if err != nil {
		log.Fatal("Error creating API client")
	}

	ircClient, err := twitch.NewIRCClient(botName, tokenManager)
	if err != nil {
		log.Fatal(err)
	}
	ircClient.Capabilities = append(ircClient.Capabilities, twitchIRC.MembershipCapability)

	appInstance := app.New(ircClient, apiClient, db)

	app.StartWebServer(appInstance, tokenManager)

	channels, err := appInstance.PrepareChannels()
	if err != nil {
		log.Fatal(err)
	}

	raffleManager := &app.RaffleManager{DB: db}

	ircClient.OnSelfJoinMessage(func(message twitchIRC.UserJoinMessage) {
		go func() {
			channelName := message.Channel
			log.Printf("Joined %s", channelName)
			apiClient, err := twitch.NewApiClient(channelName, tokenManager)
			if err != nil {
				log.Fatal(err)
			}

			channel := channels[channelName]
			channel.ApiClient = apiClient
			_, err = db.WriteInitialData(channel.ID, apiClient)
			if err != nil {
				log.Fatal(err)
			}

			for {
				time.Sleep(5 * time.Minute)
				if channel.IsLive {
					onlineVips, offlineVips, err := app.GetOnlineOfflineVips(ircClient, apiClient, channelName, channel.ID)
					if err != nil {
						log.Print(err)
					} else {
						err = db.UpdatePresenceData(channelName, onlineVips, offlineVips)
						if err != nil {
							log.Print(err)
						}
					}
				}
			}
		}()
	})

	ircClient.OnPrivateMessage(func(message twitchIRC.PrivateMessage) {
		channelName := message.Channel
		channel := channels[channelName]
		msgAuthorName := message.User.DisplayName
		msgAuthorID := message.User.ID
		prefix := "!raffle vip"

		if strings.HasPrefix(message.Message, prefix) {
			if message.User.Name != channelName {
				return
			}

			channel.Raffle.EnrollMsg = strings.TrimSpace(strings.TrimPrefix(message.Message, prefix))
			channel.Raffle.IsActive = true

			moderators, err := channel.ApiClient.GetModerators(channel.ID)
			if err != nil {
				log.Print(err)
				return
			}

			for _, moderator := range moderators {
				channel.Raffle.Ineligible[moderator.UserID] = app.RaffleParticipant{
					ID:   moderator.UserID,
					Name: moderator.UserName,
				}
			}

			channel.Raffle.Ineligible[msgAuthorID] = app.RaffleParticipant{
				ID:   msgAuthorID,
				Name: msgAuthorName,
			}

			ircClient.Say(channelName, fmt.Sprintf("Raffle begins! Send %s to chat to participate", channel.Raffle.EnrollMsg))

			time.AfterFunc(30*time.Second, func() {
				resultMsg, err := raffleManager.PickWinner(channel)
				if err != nil {
					log.Print(err)
				}

				ircClient.Say(channelName, resultMsg)
			})

			return
		}

		if channel.Raffle.IsActive && message.Message == channel.Raffle.EnrollMsg {
			if _, ok := channel.Raffle.Ineligible[msgAuthorID]; ok {
				return
			}

			channel.Raffle.Participants[msgAuthorID] = app.RaffleParticipant{
				ID:   msgAuthorID,
				Name: msgAuthorName,
			}
			log.Printf("%s joined the raffle", msgAuthorName)
		}
	})

	ircClient.Join(slices.Collect(maps.Keys(channels))...)

	app.StartTwitchWSCommunication(apiClient, channels, app.ReconnParams{})

	err = ircClient.Connect()
	if errors.Is(err, twitchIRC.ErrLoginAuthenticationFailed) {
		ircClient.Reconnect(botName, tokenManager)
	}	else if err != nil {
		log.Fatalf("Error connecting to Twitch: %v", err)
	}
}
