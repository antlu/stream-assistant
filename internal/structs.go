package structs

import (
	"sync"
	"time"
)

type User struct {
	Name     string    `csv:"name"`
	LastSeen time.Time `csv:"last_seen"`
}

type Channel struct {
	ID     string
	Name   string
	UAT    string
	IsLive bool
}

type ChannelsDict map[string]*Channel

type Channels struct {
	L     sync.RWMutex
	Names []string
	Dict  ChannelsDict
}
