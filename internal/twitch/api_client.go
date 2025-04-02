package twitch

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/nicklaw5/helix/v2"
)

type ApiClient struct {
	*helix.Client
	ready chan struct{}
}

func NewApiClient(channelName string, tokenManager *TokenManager) (*ApiClient, error) {
	client, err := helix.NewClient(&helix.Options{
		ClientID:        os.Getenv("SA_CLIENT_ID"),
		ClientSecret:    os.Getenv("SA_CLIENT_SECRET"),
	})
	if err != nil {
		return nil, err
	}

	wrapper := ApiClient{Client: client, ready: make(chan struct{})}
	go wrapper.getTokens(channelName, tokenManager)

	wrapper.OnUserAccessTokenRefreshed(func(accessToken, refreshToken string) {
		tokenManager.updateStorage(channelName, accessToken, refreshToken)
	})

	return &wrapper, nil
}

func (ac ApiClient) waitUntilReady() error {
	select {
	case <-ac.ready:
		return nil
	case <-time.After(5 * time.Minute):
    return errors.New("client timeout")
	}
}

func (ac ApiClient) getTokens(channelName string, tokenManager *TokenManager) {
	var (
		accessToken, refreshToken string
		err                       error
	)

	for {
		accessToken, refreshToken, err = tokenManager.ensureValidTokens(channelName)
		if err == nil {
			ac.SetUserAccessToken(accessToken)
			ac.SetRefreshToken(refreshToken)
			close(ac.ready)
			break
		}

		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("API: Waiting for %s authorization", channelName)
			time.Sleep(10 * time.Second)
			continue
		}

		log.Printf("Error getting tokens: %v", err)
		break
	}
}

func (ac ApiClient) GetUsersInfo(names ...string) ([]helix.User, error) {
	if err := ac.waitUntilReady(); err != nil {
		return nil, err
	}

	resp, err := ac.GetUsers(&helix.UsersParams{Logins: names})
	if err != nil {
		log.Print("Error getting users info")
		return nil, err
	}

	return resp.Data.Users, nil
}

func (ac ApiClient) GetVips(channelName string) ([]helix.ChannelVips, error) {
	usersInfo, err := ac.GetUsersInfo(channelName)
	if err != nil {
		return nil, err
	}

	resp, err := ac.GetChannelVips(&helix.GetChannelVipsParams{
		BroadcasterID: usersInfo[0].ID,
		First:         100,
	})
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Error getting VIPs of %s", channelName)
		if err == nil {
			err = errors.New(resp.ErrorMessage)
		}
		return nil, err
	}

	return resp.Data.ChannelsVips, nil
}

func (ac ApiClient) GetLiveStreams(logins []string) (map[string]bool, error) {
	if err := ac.waitUntilReady(); err != nil {
		return nil, err
	}

	streamsResp, err := ac.GetStreams(&helix.StreamsParams{UserLogins: logins})
	if err != nil {
		return nil, fmt.Errorf("error getting streams info: %v", err)
	}

	streamData := make(map[string]bool)
	for _, stream := range streamsResp.Data.Streams {
		streamData[stream.UserLogin] = true
	}

	return streamData, nil
}
