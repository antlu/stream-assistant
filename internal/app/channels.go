package app

import (
	"log"
	"regexp"
	"strings"

	"github.com/nicklaw5/helix/v2"

	"github.com/antlu/stream-assistant/internal"
)

func PrepareChannels(channelUatPairs string, apiClient *helix.Client) *types.Channels {
	validateChannelUatPairs(channelUatPairs)

	splitChannelUatPairs := strings.Split(channelUatPairs, ",")
	numberOfChannels := len(splitChannelUatPairs)
	channels := &types.Channels{
		Names: make([]string, 0, numberOfChannels),
		Dict:  make(types.ChannelsDict, numberOfChannels),
	}
	for _, pair := range splitChannelUatPairs {
		parts := strings.Split(pair, ":")
		channels.Dict[parts[0]] = &types.Channel{
			Name: parts[0], UAT: parts[1],
			Raffle: types.Raffle{
				Participants: make(types.IDRaffleParticipantDict),
				Ineligible:   make(types.IDRaffleParticipantDict),
			},
		}
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

func validateChannelUatPairs(channelUatPairs string) {
	matched, err := regexp.MatchString(`^\w+:\w+(?:,(?:\w+:\w+))*?$`, channelUatPairs)
	if err != nil {
		log.Fatal(err)
	}

	if matched {
		return
	}

	log.Fatal("Invalid channel:user_access_token pairs")
}
