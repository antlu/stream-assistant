package types

import (
	"sync"
	"time"
)

type User struct {
	Name     string    `csv:"name"`
	LastSeen time.Time `csv:"last_seen"`
}

type IDNameDict map[string]string

type Raffle struct {
	IsActive     bool
	EnrollMsg    string
	Participants IDNameDict
	Ineligible   IDNameDict
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
