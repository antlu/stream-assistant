package app

import (
	"encoding/json"
	"log"
	"time"

	"github.com/lxzan/gws"
	"github.com/nicklaw5/helix/v2"

	"github.com/antlu/stream-assistant/internal"
)

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
		Event struct {
			ID                   string    `json:"id"`
			BroadcasterUserID    string    `json:"broadcaster_user_id"`
			BroadcasterUserLogin string    `json:"broadcaster_user_login"`
			BroadcasterUserName  string    `json:"broadcaster_user_name"`
			Type                 string    `json:"type"`
			StartedAt            time.Time `json:"started_at"`
		} `json:"event"`
	} `json:"payload"`
}

type ReconnParams struct {
	ReconnectUrl string
	closeOldConn func()
}

type handler struct {
	*helix.Client
	channels     *types.Channels
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
	json.Unmarshal(message.Data.Bytes(), &msg)

	switch msg.Metadata.MessageType {
	case "session_welcome":
		if h.closeOldConn != nil {
			h.closeOldConn()
			h.closeOldConn = nil
			return
		}

		createSub := createSubRequester(h.Client, msg.Payload.Session.ID)
		for _, channel := range h.channels.Dict {
			go func() {
				createSub(channel.ID, helix.EventSubTypeStreamOnline)
				createSub(channel.ID, helix.EventSubTypeStreamOffline)
			}()
		}
	case "session_keepalive":
		// log.Print("Keepalive message")
	case "notification":
		h.switchChannelLiveStatus(msg.Payload.Event.BroadcasterUserLogin, msg.Payload.Subscription.Type)
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

func (h handler) switchChannelLiveStatus(channelName, status string) {
	var isLive bool

	switch status {
	case helix.EventSubTypeStreamOnline:
		isLive = true
	case helix.EventSubTypeStreamOffline:
		isLive = false
	default:
		log.Printf("Unknown channel status: %s (%s)", status, channelName)
		return
	}

	h.channels.Dict[channelName].IsLive = isLive
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

func StartTwitchWSCommunication(apiClient *helix.Client, channels *types.Channels, params ReconnParams) {
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
