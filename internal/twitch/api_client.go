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
	channelName string
}

func NewApiClient(accessToken, refreshToken string) (*helix.Client, error) {
	client, err := helix.NewClient(&helix.Options{
		ClientID:        os.Getenv("SA_CLIENT_ID"),
		ClientSecret:    os.Getenv("SA_CLIENT_SECRET"),
		UserAccessToken: accessToken,
		RefreshToken:    refreshToken,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func NewApiClientWithChannel(channelName string, tokenManager *TokenManager) (*ApiClient, error) {
	underlyingClient, err := NewApiClient("", "")
	if err != nil {
		return nil, err
	}

	client := ApiClient{underlyingClient, channelName}
	go client.waitForTokens(tokenManager)

	client.OnUserAccessTokenRefreshed(func(accessToken, refreshToken string) {
		tokenManager.updateStorage(channelName, accessToken, refreshToken)
	})

	return &client, nil
}

func (ac ApiClient) hasTokens() bool {
	return ac.GetUserAccessToken() != "" && ac.GetRefreshToken() != ""
}

func (ac ApiClient) waitUntilReady(duration time.Duration) {
	for !ac.hasTokens() {
		time.Sleep(duration)
	}
}


func (ac ApiClient) waitForTokens(tokenManager *TokenManager) {
	var (
		accessToken, refreshToken string
		err                       error
	)

	for {
		accessToken, refreshToken, err = tokenManager.ensureValidTokens(ac.channelName)
		if err == nil {
			ac.SetUserAccessToken(accessToken)
			ac.SetRefreshToken(refreshToken)
			break
		}

		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("API: Waiting for %s authorization", ac.channelName)
			time.Sleep(10 * time.Second)
			continue
		}

		log.Printf("Error getting tokens: %v", err)
	}
}

func (ac ApiClient) GetUsersInfo(names ...string) ([]helix.User, error) {
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
