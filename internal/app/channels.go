package app

import (
	"encoding/hex"
	"log"
	"os"

	"github.com/gtank/cryptopasta"
	"github.com/nicklaw5/helix/v2"

	"github.com/antlu/stream-assistant/internal"
)

func PrepareChannels(apiClient *helix.Client) types.ChannelsDict {
	db := openDB()
	defer db.Close()
	var numberOfChannels int
	db.QueryRow("SELECT COUNT(*) FROM channels").Scan(&numberOfChannels)
	channels := make(types.ChannelsDict, numberOfChannels)
	channelNames := make([]string, 0, numberOfChannels)
	rows, err := db.Query("SELECT login, access_token FROM channels")
	if err != nil {
		log.Fatal(err)
	}

	secureKey, err := hex.DecodeString(os.Getenv("SA_SECURE_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	secureKeyPointer := (*[32]byte)(secureKey)

	for rows.Next() {
		var login, accessToken string
		if err := rows.Scan(&login, &accessToken); err != nil {
			log.Fatal(err)
		}

		decodedAccessToken, err := hex.DecodeString(accessToken)
		if err != nil {
			log.Fatal(err)
		}
		decryptedAccessToken, err := cryptopasta.Decrypt(decodedAccessToken, secureKeyPointer)
		if err != nil {
			log.Fatal(err)
		}

		channels[login] = &types.Channel{
			Name: login,
			UAT: string(decryptedAccessToken),
			Raffle: types.Raffle{
				Participants: make(types.IDRaffleParticipantDict),
				Ineligible:   make(types.IDRaffleParticipantDict),
			},
		}
		channelNames = append(channelNames, login)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	// Get channel IDs
	usersResp, err := apiClient.GetUsers(&helix.UsersParams{Logins: channelNames})
	if err != nil {
		log.Fatal("Error getting users info")
	}
	for _, user := range usersResp.Data.Users {
		channels[user.Login].ID = user.ID
	}

	// Get live streams
	streamsResp, err := apiClient.GetStreams(&helix.StreamsParams{UserLogins: channelNames})
	if err != nil {
		log.Fatal("Error getting streams info")
	}
	for _, stream := range streamsResp.Data.Streams {
		channels[stream.UserLogin].IsLive = true
	}

	return channels
}
