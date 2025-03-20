package main

import (
	"errors"
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

	types "github.com/antlu/stream-assistant/internal"
	"github.com/antlu/stream-assistant/internal/app"
	"github.com/antlu/stream-assistant/internal/crypto"
	"github.com/antlu/stream-assistant/internal/twitch"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	app.StartWebServer()

	db := app.OpenDB()
	defer db.Close()
	tokenManager := twitch.NewTokenManager(db, crypto.Cipher(os.Getenv("SA_SECURE_KEY")))
	botName := os.Getenv("SA_BOT_NAME")

	apiClient, err := twitch.NewApiClient(botName, tokenManager)
	if err != nil {
		log.Fatal("Error creating API client")
	}

	channels := app.PrepareChannels(apiClient)

	ircClient, err := twitch.NewIRCClient(botName, tokenManager)
	if err != nil {
		log.Fatal(err)
	}
	ircClient.Capabilities = append(ircClient.Capabilities, twitchIRC.MembershipCapability)

	apiClientForChannel := make(map[string]*twitch.ApiClient)

	ircClient.OnSelfJoinMessage(func(message twitchIRC.UserJoinMessage) {
		go func() {
			channelName := message.Channel
			log.Printf("Joined %s", channelName)
			apiClient, err := twitch.NewApiClientWithChannel(channelName, channels[channelName].UAT)
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
				channel.Raffle.Ineligible[moderator.UserID] = types.RaffleParticipant{
					ID:   moderator.UserID,
					Name: moderator.UserName,
				}
			}
			channel.Raffle.Ineligible[msgAuthorID] = types.RaffleParticipant{
				ID:   msgAuthorID,
				Name: msgAuthorName,
			}
			ircClient.Say(channelName, fmt.Sprintf("Розыгрыш начался! Для участия отправь в чат %s", channel.Raffle.EnrollMsg))

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
					loser  types.RaffleParticipant
					winner types.RaffleParticipant
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
					log.Print("No one won")
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

						loser = types.RaffleParticipant{ID: users[0].ID, Name: users[0].DisplayName}
					}
				}

				unvipMsg := ""
				if loser.ID != "" {
					unvipMsg = fmt.Sprintf("%s потерял випку. ", loser.Name)
				}
				resultMsg := fmt.Sprintf("%sНовый вип — %s!", unvipMsg, winner.Name)
				ircClient.Say(channelName, resultMsg)
			})

			return
		}

		if channel.Raffle.IsActive && message.Message == channel.Raffle.EnrollMsg {
			if _, ok := channel.Raffle.Ineligible[msgAuthorID]; ok {
				return
			}

			channel.Raffle.Participants[msgAuthorID] = types.RaffleParticipant{
				ID:   msgAuthorID,
				Name: msgAuthorName,
			}
			log.Printf("%s joined the raffle", msgAuthorName)
		}
	})

	ircClient.Join(slices.Collect(maps.Keys(channels))...)

	app.StartTwitchWSCommunication(apiClient, channels, app.ReconnParams{})

	for {
		err = ircClient.Connect()
		if errors.Is(err, twitchIRC.ErrLoginAuthenticationFailed) {
			ircClient.SetIRCToken()
		}	else if err != nil {
			log.Fatal("Error connecting to Twitch: %v", err)
		}
	}
}
