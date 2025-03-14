package app

import (
	"encoding/json"
	"log"
	"time"

	"github.com/lxzan/gws"
	"github.com/nicklaw5/helix/v2"

	"github.com/antlu/stream-assistant/internal"
)

// TODO: split into different types
type PayloadEvent struct {
	ID                   string    `json:"id"`
	UserID               string    `json:"user_id"`
	UserLogin            string    `json:"user_login"`
	UserName             string    `json:"user_name"`
	BroadcasterUserID    string    `json:"broadcaster_user_id"`
	BroadcasterUserLogin string    `json:"broadcaster_user_login"`
	BroadcasterUserName  string    `json:"broadcaster_user_name"`
	Type                 string    `json:"type"`
	StartedAt            time.Time `json:"started_at"`
}

type incomingMessage struct {
	Metadata struct {
		MessageID           string    `json:"message_id"`
		MessageType         string    `json:"message_type"`
		MessageTimestamp    time.Time `json:"message_timestamp"`
		SubscriptionType    string    `json:"subscription_type"`
		SubscriptionVersion string    `json:"subscription_version"`
	} `json:"metadata"`
	Payload struct {
		Session struct {
			ID                      string    `json:"id"`
			Status                  string    `json:"status"`
			KeepaliveTimeoutSeconds int       `json:"keepalive_timeout_seconds"`
			ReconnectUrl            string    `json:"reconnect_url"`
			ConnectedAt             time.Time `json:"connected_at"`
		} `json:"session"`
		Subscription struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			Type      string `json:"type"`
			Version   string `json:"version"`
			Cost      int    `json:"cost"`
			Condition struct {
				BroadcasterUserID string `json:"broadcaster_user_id"`
			}
			Transport struct {
				Method    string `json:"method"`
				SessionID string `json:"session_id"`
			}
			CreatedAt time.Time `json:"created_at"`
		} `json:"subscription"`
		Event PayloadEvent `json:"event"`
	} `json:"payload"`
}

const (
	streamOnline  = "stream.online"
	streamOffline = "stream.offline"
	channelVipAdd = "channel.vip.add"
)

var eventSubTypes = []string{streamOnline, streamOffline, channelVipAdd}

type ReconnParams struct {
	ReconnectUrl string
	closeOldConn func()
}

type handler struct {
	*helix.Client
	channels     types.ChannelsDict
	closeOldConn func()
}

func (h handler) OnOpen(conn *gws.Conn) {
	log.Print("WebSocket connection opened")
}

func (h handler) OnClose(conn *gws.Conn, err error) {
	log.Printf("WebSocket connection closed: %v", err)
}

func (h handler) OnPing(conn *gws.Conn, payload []byte) {
	// log.Print("Got ping")
	conn.WritePong(payload)
}

func (h handler) OnPong(conn *gws.Conn, payload []byte) {
}

func (h handler) OnMessage(conn *gws.Conn, message *gws.Message) {
	msg := incomingMessage{}
	json.Unmarshal(message.Bytes(), &msg)

	switch msg.Metadata.MessageType {
	case "session_welcome":
		if h.closeOldConn != nil {
			h.closeOldConn()
			h.closeOldConn = nil
			return
		}

		createSub := createSubRequester(h.Client, msg.Payload.Session.ID)
		for _, channel := range h.channels {
			go func() {
				for _, subType := range eventSubTypes {
					createSub(channel.ID, subType)
				}
			}()
		}
	case "session_keepalive":
		// log.Print("Keepalive message")
	case "notification":
		h.handleNotification(msg.Payload.Event, msg.Payload.Subscription.Type)
	case "session_reconnect":
		// log.Print("Reconnection requested")
		StartTwitchWSCommunication(
			h.Client,
			h.channels,
			ReconnParams{msg.Payload.Session.ReconnectUrl, func() {
				conn.WriteClose(1000, []byte("Old connection"))
			}},
		)
	case "revocation":
		log.Print("Revocation message")
	default:
		log.Printf("Unknown message type: %s", msg.Metadata.MessageType)
	}

	message.Close()
}

func (h handler) handleNotification(event PayloadEvent, subType string) {
	channelName := event.BroadcasterUserLogin

	switch subType {
	case streamOnline:
		h.channels[channelName].IsLive = true
	case streamOffline:
		h.channels[channelName].IsLive = false
	case channelVipAdd:
		appendUserToFile(channelName, event.UserLogin)
	default:
		log.Printf("Unknown channel subscription type: %s (%s)", subType, channelName)
		return
	}
}

func createSubRequester(client *helix.Client, sessionID string) func(string, string) {
	return func(channelID, subType string) {
		_, err := client.CreateEventSubSubscription(&helix.EventSubSubscription{
			Type:      subType,
			Version:   "1",
			Condition: helix.EventSubCondition{BroadcasterUserID: channelID},
			Transport: helix.EventSubTransport{Method: "websocket", SessionID: sessionID},
		})
		if err != nil {
			log.Print(err)
		}
	}
}

func StartTwitchWSCommunication(apiClient *helix.Client, channels types.ChannelsDict, params ReconnParams) {
	serverAddr := "wss://eventsub.wss.twitch.tv/ws"
	if params.ReconnectUrl != "" {
		serverAddr = params.ReconnectUrl
	}

	conn, _, err := gws.NewClient(
		&handler{Client: apiClient, channels: channels, closeOldConn: params.closeOldConn},
		&gws.ClientOption{Addr: serverAddr},
	)
	if err != nil {
		log.Fatal(err)
	}

	go conn.ReadLoop()
}
