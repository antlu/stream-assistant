package twitch

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
)

type IRCClient struct {
	*twitch.Client
}

func NewIRCClient(channelName string, tokenManager *TokenManager) (*IRCClient, error) {
	client := IRCClient{twitch.NewClient(channelName, "")}
	go client.waitForToken(channelName, tokenManager)
	return &client, nil
}

func (c IRCClient) waitForToken(channelName string, tokenManager *TokenManager) {
	var (
		accessToken string
		err error
	)

	for {
		accessToken, _, err = tokenManager.ensureValidTokens(channelName)
		if err == nil {
			c.SetIRCToken(fmt.Sprintf("oauth:%s", accessToken))
			break
		}

		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("IRC: Waiting for %s authorization", channelName)
			time.Sleep(10 * time.Second)
			continue
		}

		log.Printf("Error getting token: %v", err)
	}
}
