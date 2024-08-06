package app

import (
	"log"
	"strings"

	"github.com/nicklaw5/helix/v2"

	"github.com/antlu/stream-assistant/internal"
)

func PrepareChannels(channelUatPairs []string, apiClient *helix.Client) *structs.Channels {
	numberOfChannels := len(channelUatPairs)
	channels := &structs.Channels{
		Names: make([]string, 0, numberOfChannels),
		Dict:  make(structs.ChannelsDict, numberOfChannels),
	}
	for _, pair := range channelUatPairs {
		parts := strings.Split(pair, ":")
		channels.Dict[parts[0]] = &structs.Channel{Name: parts[0], UAT: parts[1]}
		channels.Names = append(channels.Names, parts[0])
	}

	// Get channel IDs
	usersResp, err := apiClient.GetUsers(&helix.UsersParams{Logins: channels.Names})
	if err != nil {
		log.Fatal("Error getting users info")
	}
	for _, user := range usersResp.Data.Users {
		channels.Dict[user.Login].ID = user.ID
	}

	// Get live streams
	streamsResp, err := apiClient.GetStreams(&helix.StreamsParams{UserLogins: channels.Names})
	if err != nil {
		log.Fatal("Error getting streams info")
	}
	for _, stream := range streamsResp.Data.Streams {
		channels.Dict[stream.UserLogin].IsLive = true
	}

	return channels
}
