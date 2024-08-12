package types

import (
	"sync"
	"time"
)

type User struct {
	Name     string    `csv:"name"`
	LastSeen time.Time `csv:"last_seen"`
}

type StringSet map[string]bool

type Raffle struct {
	IsActive     bool
	EnrollMsg    string
	Participants StringSet
	Ineligible   StringSet
}

type Channel struct {
	ID     string
	Name   string
	UAT    string
	IsLive bool
	Raffle Raffle
}

type ChannelsDict map[string]*Channel

type Channels struct {
	L     sync.RWMutex
	Names []string
	Dict  ChannelsDict
}
