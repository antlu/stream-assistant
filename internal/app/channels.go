package app

import (
	"log"

	"github.com/nicklaw5/helix/v2"

	"github.com/antlu/stream-assistant/internal"
	"github.com/antlu/stream-assistant/internal/twitch"
)

func PrepareChannels(apiClient *twitch.ApiClient) types.ChannelsDict {
	db := OpenDB()
	defer db.Close()
	var numberOfChannels int
	db.QueryRow("SELECT COUNT(*) FROM channels").Scan(&numberOfChannels)
	channels := make(types.ChannelsDict, numberOfChannels)
	channelNames := make([]string, 0, numberOfChannels)
	rows, err := db.Query("SELECT login FROM channels")
	if err != nil {
		log.Fatal(err)
	}

	for rows.Next() {
		var login string
		if err := rows.Scan(&login); err != nil {
			log.Fatal(err)
		}

		channels[login] = &types.Channel{
			Name: login,
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
