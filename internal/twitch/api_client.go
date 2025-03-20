package twitch

import (
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/nicklaw5/helix/v2"
)

type ApiClient struct {
	*helix.Client
	channelName string
}

func NewApiClient(accessToken, refreshToken string) (*helix.Client, error){
	client, err := helix.NewClient(&helix.Options{
		ClientID:        "jmaoofuyr1c4v8lqzdejzfppdj5zym",
		ClientSecret:    os.Getenv("SA_CLIENT_SECRET"),
		UserAccessToken: accessToken,
		RefreshToken:    refreshToken,
	})
	if err != nil {
		return nil, err
	}

	client.OnUserAccessTokenRefreshed()
	return client, nil
}

func NewApiClientWithChannel(channelName, accessToken string) (*ApiClient, error) {
	client, err := NewApiClient(accessToken)
	if err != nil {
		return nil, err
	}
	return &ApiClient{client, channelName}, nil
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
