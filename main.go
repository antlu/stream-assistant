package main

import (
	"fmt"
	"log"
	"maps"
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

	apiClient, err := twitch.NewApiClientWithChannel(botName, tokenManager)
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

	apiClient.WaitUntilReady(10 * time.Second)

	channels, err := appInstance.PrepareChannels()
	if err != nil {
		log.Fatal(err)
	}

	apiClientForChannel := make(map[string]*twitch.ApiClient)

	ircClient.OnSelfJoinMessage(func(message twitchIRC.UserJoinMessage) {
		go func() {
			channelName := message.Channel
			log.Printf("Joined %s", channelName)
			apiClient, err := twitch.NewApiClientWithChannel(channelName, tokenManager)
			if err != nil {
				log.Fatal(err)
			}
			apiClientForChannel[channelName] = apiClient
			app.WriteInitialDataToUsersFile(channelName, apiClient)

			for {
				time.Sleep(5 * time.Minute)
				if channels[channelName].IsLive {
					onlineVips, offlineVips, err := app.GetOnlineOfflineVips(ircClient, apiClient, channelName)
					if err != nil {
						log.Print(err)
					} else {
						app.UpdateUsersFile(channelName, onlineVips, offlineVips)
					}
				}
			}
		}()
	})

	ircClient.OnPrivateMessage(func(message twitchIRC.PrivateMessage) {
		channelName := message.Channel
		channel := channels[channelName]
		apiClient := apiClientForChannel[channelName]
		msgAuthorName := message.User.DisplayName
		msgAuthorID := message.User.ID
		prefix := "!raffle vip"

		if strings.HasPrefix(message.Message, prefix) {
			if message.User.Name != channelName {
				return
			}

			channel.Raffle.EnrollMsg = strings.TrimSpace(strings.TrimPrefix(message.Message, prefix))
			channel.Raffle.IsActive = true

			resp, err := apiClient.GetModerators(&helix.GetModeratorsParams{BroadcasterID: channel.ID})
			if err != nil || resp.StatusCode != http.StatusOK {
				log.Print("Error getting moderators")
				return
			}

			for _, moderator := range resp.Data.Moderators {
				channel.Raffle.Ineligible[moderator.UserID] = app.RaffleParticipant{
					ID:   moderator.UserID,
					Name: moderator.UserName,
				}
			}
			channel.Raffle.Ineligible[msgAuthorID] = app.RaffleParticipant{
				ID:   msgAuthorID,
				Name: msgAuthorName,
			}
			ircClient.Say(channelName, fmt.Sprintf("The raffle begins! Send %s to the chat to participate", channel.Raffle.EnrollMsg))

			time.AfterFunc(30*time.Second, func() {
				channel.Raffle.IsActive = false

				participantIDs := slices.Collect(maps.Keys(channel.Raffle.Participants))

				rand.Shuffle(len(participantIDs), func(i, j int) {
					participantIDs[i], participantIDs[j] = participantIDs[j], participantIDs[i]
				})

				vips, err := apiClient.GetVips(channelName)
				if err != nil {
					log.Print(err)
					return
				}

				vipIDs := make([]string, 0, len(vips))
				for _, vip := range vips {
					vipIDs = append(vipIDs, vip.UserID)
				}

				var (
					loser  app.RaffleParticipant
					winner app.RaffleParticipant
				)

				for _, participantID := range participantIDs {
					if !slices.Contains(vipIDs, participantID) {
						winner = channel.Raffle.Participants[participantID]
						break
					}
					if loser.ID == "" {
						loser = channel.Raffle.Participants[participantID]
					}
				}

				if winner.ID == "" {
					log.Print("No one has won")
					return
				}

				for i := 0; i < 2; i++ {
					log.Printf("VIPs routine: attempt %d", i+1)

					if loser.ID != "" {
						_, err := apiClient.RemoveChannelVip(&helix.RemoveChannelVipParams{
							UserID:        loser.ID,
							BroadcasterID: channel.ID,
						})
						if err != nil {
							log.Print(err)
						}

						log.Printf("Demoted %s", loser.Name)
					}

					resp, err := apiClient.AddChannelVip(&helix.AddChannelVipParams{
						UserID:        winner.ID,
						BroadcasterID: channel.ID,
					})
					if err != nil {
						log.Print(err)
					}
					if resp.StatusCode == http.StatusNoContent {
						log.Printf("Promoted %s", winner.Name)
						break
					}
					if resp.StatusCode == http.StatusConflict {
						log.Print("No free slots. Will search who to demote")
						userFromFile := app.GetFirstUserFromFile(channelName)
						users, err := apiClient.GetUsersInfo(userFromFile)
						if err != nil {
							log.Print(err)
						}

						loser = app.RaffleParticipant{ID: users[0].ID, Name: users[0].DisplayName}
					}
				}

				unvipMsg := ""
				if loser.ID != "" {
					unvipMsg = fmt.Sprintf("%s has lost their status. ", loser.Name)
				}
				resultMsg := fmt.Sprintf("%sNew VIP — %s!", unvipMsg, winner.Name)
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
	if err != nil {
		log.Fatalf("Error connecting to Twitch: %v", err)
	}
}
