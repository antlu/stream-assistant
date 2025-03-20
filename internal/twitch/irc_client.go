package twitch

import (
	"fmt"

	"github.com/gempir/go-twitch-irc/v4"
)

type IRCClient struct {
	*twitch.Client
	tokenManager *TokenManager
}

func NewIRCClient(username string, tokenManager *TokenManager) (*IRCClient, error) {
	accessToken, err := tokenManager.getValidAccessToken(username)
	if err != nil {
		return nil, fmt.Errorf("error getting valid access token: %w", err)
	}

	ircClient := twitch.NewClient(username, fmt.Sprintf("oauth:%s", accessToken))
	return &IRCClient{Client: ircClient, tokenManager: tokenManager}, nil
}

func (c *IRCClient) RefreshToken() {
	c.tokenManager.getValidAccessToken(c.ircUser)
}
