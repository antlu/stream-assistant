package main

import (
	"log"
	"math/rand"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	twitchIRC "github.com/gempir/go-twitch-irc/v4"
	"github.com/joho/godotenv"
	"github.com/nicklaw5/helix/v2"

	"github.com/antlu/stream-assistant/internal/app"
	"github.com/antlu/stream-assistant/internal/twitch"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	nick := os.Getenv("SA_NICK")
	pass := os.Getenv("SA_PASS")
	channelUatPairs := os.Getenv("SA_CHANNEL_UAT_PAIRS")

	apiClient, err := helix.NewClient(&helix.Options{
		ClientID:        "jmaoofuyr1c4v8lqzdejzfppdj5zym",
		UserAccessToken: os.Getenv("SA_USER_ACCESS_TOKEN"),
	})
	if err != nil {
		log.Fatal("Error creating API client")
	}

	channels := app.PrepareChannels(channelUatPairs, apiClient)

	apiClientForChannel := make(map[string]*twitch.ApiClient)

	ircClient := twitchIRC.NewClient(nick, pass)
	ircClient.Capabilities = append(ircClient.Capabilities, twitchIRC.MembershipCapability)

	ircClient.OnSelfJoinMessage(func(message twitchIRC.UserJoinMessage) {
		go func() {
			channelName := message.Channel
			log.Printf("Joined %s", channelName)
			apiClient := twitch.NewApiClient(channelName, channels.Dict[channelName].UAT)
			apiClientForChannel[channelName] = apiClient

			vipNames, err := apiClient.GetVipNames(channelName)
			if err != nil {
				log.Fatal(err)
			}
			app.WriteDataToUsersFileIfNotExists(channelName, vipNames)

			for {
				time.Sleep(5 * time.Minute)
				if channels.Dict[channelName].IsLive {
					onlineVips, err := app.GetOnlineVips(ircClient, apiClient, channelName)
					if err != nil {
						log.Print(err)
					} else {
						app.UpdateUsersFile(channelName, onlineVips)
					}
				}
			}
		}()
	})

	ircClient.OnPrivateMessage(func(message twitchIRC.PrivateMessage) {
	// 	ircClient.Say(message.Channel, "hey")
	// log.Printf("%s: %s", message.User.Name, message.Message)
		channelName := message.Channel
		channel := channels.Dict[channelName]
		msgAuthorName := message.User.Name
		prefix := "!raffle vip"
		if strings.HasPrefix(message.Message, prefix) {
			if msgAuthorName != channelName {
				return
			}

			channel.Raffle.EnrollMsg = strings.TrimSpace(strings.TrimPrefix(message.Message, prefix))
			channel.Raffle.IsActive = true

			resp, err := apiClientForChannel[channelName].GetModerators(&helix.GetModeratorsParams{BroadcasterID: channel.ID})
			if err != nil || resp.StatusCode != http.StatusOK {
				log.Print("Error getting moderators")
				return
			}

			for _, moderator := range resp.Data.Moderators {
				channel.Raffle.Ineligible[moderator.UserLogin] = true
			}
			channel.Raffle.Ineligible[msgAuthorName] = true

			time.AfterFunc(30 * time.Second, func() {
				channel.Raffle.IsActive = false

				names := make([]string, 0, len(channel.Raffle.Participants))
				for name := range channel.Raffle.Participants {
					names = append(names, name)
				}

				rand.Shuffle(len(names), func(i, j int) {
					names[i], names[j] = names[j], names[i]
				})

				vipNames, err := apiClientForChannel[channelName].GetVipNames(channelName)
				if err != nil {
					log.Print(err)
					return
				}

				unvipTarget := ""
				winner := ""
				for _, name := range names {
					if !slices.Contains(vipNames, name) {
						winner = name
						break
					}
					if unvipTarget == "" {
						unvipTarget = name
					}
				}

				if unvipTarget == "" {
					userFromFile := app.GetFirstUserFromFile(channelName)
					unvipTarget = userFromFile
				}

				log.Printf("%+v", channel.Raffle) // TODO: remove
				log.Printf("unvip: %s, vip: %s", unvipTarget, winner) // TODO: remove
			})

			return
		}

		if channel.Raffle.IsActive && message.Message == channel.Raffle.EnrollMsg {
			if channel.Raffle.Ineligible[msgAuthorName] {
				return
			}

			channel.Raffle.Participants[msgAuthorName] = true
			log.Printf("%+v", channel.Raffle) // TODO: remove
		}
	})

	ircClient.Join(channels.Names...)

	app.StartTwitchWSCommunication(apiClient, channels)

	err = ircClient.Connect()
	if err != nil {
		log.Fatal("Error connecting to Twitch")
	}
}
