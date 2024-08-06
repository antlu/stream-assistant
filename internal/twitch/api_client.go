package twitch

import (
	"errors"
	"log"
	"net/http"

	"github.com/nicklaw5/helix/v2"
)

type ApiClient struct {
	*helix.Client
	channel string
}

func NewApiClient(channel, uat string) *ApiClient {
	client, err := helix.NewClient(&helix.Options{
		ClientID:        "jmaoofuyr1c4v8lqzdejzfppdj5zym",
		UserAccessToken: uat,
	})
	if err != nil {
		log.Fatalf("Error creating API client for %s", channel)
	}

	return &ApiClient{client, channel}
}

func (ac ApiClient) getUsersInfo(names ...string) ([]helix.User, error) {
	resp, err := ac.GetUsers(&helix.UsersParams{Logins: names})

	if err != nil {
		log.Print("Error getting users info")
		return nil, err
	}

	return resp.Data.Users, nil
}

func (ac ApiClient) GetVipNames(channelName string) ([]string, error) {
	usersInfo, err := ac.getUsersInfo(channelName)
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

	vips := make([]string, 0, len(resp.Data.ChannelsVips))
	for _, vip := range resp.Data.ChannelsVips {
		vips = append(vips, vip.UserLogin)
	}

	return vips, nil
}
