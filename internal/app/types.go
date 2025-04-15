package app

import (
	"sync"
	"time"

	"github.com/antlu/stream-assistant/internal/twitch"
)

type User struct {
	Name     string    `csv:"name"`
	LastSeen time.Time `csv:"last_seen"`
}

type RaffleParticipant struct {
	ID   string
	Name string
}

type IDRaffleParticipantDict map[string]RaffleParticipant

type Raffle struct {
	IsActive     bool
	EnrollMsg    string
	Participants IDRaffleParticipantDict
	Ineligible   IDRaffleParticipantDict
}

type Channel struct {
	ID     string
	Name   string
	IsLive bool
	Raffle Raffle
	ApiClient *twitch.ApiClient
}

type ChannelsDict map[string]*Channel

type Channels struct {
	L     sync.RWMutex
	Names []string
	Dict  ChannelsDict
}
