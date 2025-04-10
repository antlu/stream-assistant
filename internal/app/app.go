package app

import (
	"fmt"

	"github.com/antlu/stream-assistant/internal/interfaces"
	"github.com/antlu/stream-assistant/internal/twitch"
)

type channelParams struct {
	id   string
	name string
}

type App struct {
	ircClient *twitch.IRCClient
	apiClient *twitch.ApiClient
	db        interfaces.DBQueryExecCloser
	channels  ChannelsDict
}

func New(ircClient *twitch.IRCClient, apiClient *twitch.ApiClient, db interfaces.DBQueryExecCloser) *App {
	return &App{
		ircClient: ircClient,
		apiClient: apiClient,
		db:        db,
		channels:  make(ChannelsDict),
	}
}
func (a *App) PrepareChannels() (ChannelsDict, error) {
	var channelNames []string

	rows, err := a.db.Query("SELECT id, login FROM channels")
	if err != nil {
		return nil, fmt.Errorf("error querying channels: %v", err)
	}
	for rows.Next() {
		var id, login string
		if err := rows.Scan(&id, &login); err != nil {
			return nil, fmt.Errorf("error scanning channel login: %v", err)
		}

		a.channels[login] = a.makeChannelBase(channelParams{id: id, name: login})
		channelNames = append(channelNames, login)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %v", err)
	}

	streamData, err := a.apiClient.GetLiveStreams(channelNames)
	if err != nil {
		return nil, fmt.Errorf("error fetching streams data: %v", err)
	}
	for login, isLive := range streamData {
		a.channels[login].IsLive = isLive
	}

	return a.channels, nil
}

func (a *App) addChannel(id, name string) error {
	a.channels[name] = a.makeChannelBase(channelParams{id: id, name: name})

	streamData, err := a.apiClient.GetLiveStreams([]string{name})
	if err != nil {
		return fmt.Errorf("error fetching stream data: %v", err)
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
