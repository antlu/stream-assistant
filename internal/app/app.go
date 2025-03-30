package app

import (
	"database/sql"
	"fmt"

	"github.com/nicklaw5/helix/v2"

	"github.com/antlu/stream-assistant/internal/twitch"
)

type channelParams struct {
	id   string
	name string
}

type App struct {
	ircClient *twitch.IRCClient
	apiClient *twitch.ApiClient
	db        *sql.DB
	channels  ChannelsDict
}

func New(ircClient *twitch.IRCClient, apiClient *twitch.ApiClient, db *sql.DB) *App {
	return &App{
		ircClient: ircClient,
		apiClient: apiClient,
		db:        db,
		channels:  make(ChannelsDict),
	}
}

func (a *App) fetchStreamData(logins []string) (map[string]bool, error) {
	streamsResp, err := a.apiClient.GetStreams(&helix.StreamsParams{UserLogins: logins})
	if err != nil {
		return nil, fmt.Errorf("error getting streams info: %w", err)
	}

	streamData := make(map[string]bool)
	for _, stream := range streamsResp.Data.Streams {
		streamData[stream.UserLogin] = true
	}

	return streamData, nil
}

func (a *App) PrepareChannels() (ChannelsDict, error) {
	var channelNames []string

	rows, err := a.db.Query("SELECT id, login FROM channels")
	if err != nil {
		return nil, fmt.Errorf("error querying channels: %w", err)
	}

	for rows.Next() {
		var id, login string
		if err := rows.Scan(&id, &login); err != nil {
			return nil, fmt.Errorf("error scanning channel login: %w", err)
		}

		a.channels[login] = a.makeChannelBase(channelParams{id: id, name: login})
		channelNames = append(channelNames, login)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	streamData, err := a.fetchStreamData(channelNames)
	if err != nil {
		return nil, fmt.Errorf("error fetching streams data: %w", err)
	}
	for login, isLive := range streamData {
		a.channels[login].IsLive = isLive
	}

	return a.channels, nil
}

func (a *App) addChannel(id, name string) error {
	a.channels[name] = a.makeChannelBase(channelParams{id: id, name: name})

	streamData, err := a.fetchStreamData([]string{name})
	if err != nil {
		return fmt.Errorf("error fetching stream data: %w", err)
	}
	a.channels[name].IsLive = streamData[name]

	return nil
}

func (*App) makeChannelBase(params channelParams) *Channel {
	channel := &Channel{
		ID:   params.id,
		Name: params.name,
		Raffle: Raffle{
			Participants: make(IDRaffleParticipantDict),
			Ineligible:   make(IDRaffleParticipantDict),
		},
	}
	return channel
}
