package internal

import (
	"encoding/json"
	"log"
	"time"

	"github.com/lxzan/gws"
	"github.com/nicklaw5/helix/v2"
)

type handler struct {
	*helix.Client
	channels channelsDict
}

func (h handler) OnOpen(conn *gws.Conn) {
	log.Print("WebSocket connection opened")
}

func (h handler) OnClose(conn *gws.Conn, err error) {
	log.Printf("WebSocket connection closed: %v", err)
}

func (h handler) OnPing(conn *gws.Conn, payload []byte) {
	log.Print("Got ping")
	conn.WritePong(payload)
}

func (h handler) OnPong(conn *gws.Conn, payload []byte) {
}

func (h handler) OnMessage(conn *gws.Conn, message *gws.Message) {
	msg := incomingMessage{}
	json.Unmarshal(message.Data.Bytes(), &msg)
	log.Print(msg)

	switch msg.Metadata.MessageType {
	case "session_welcome":
		createSub := createSubRequester(h.Client, msg.Payload.Session.ID)
		for _, channel := range h.channels {
			createSub(channel.ID, helix.EventSubTypeStreamOnline)
			createSub(channel.ID, helix.EventSubTypeStreamOffline)
		}
	case "session_keepalive":
	case "notification":
	case "session_reconnect":
	case "revocation":
	default:
		log.Printf("Unknown message type: %s", msg.Metadata.MessageType)
	}

	message.Close()
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

func StartTwitchWSCommunication(apiClient *helix.Client, channels channelsDict) {
	conn, _, err := gws.NewClient(handler{apiClient, channels}, &gws.ClientOption{
		Addr: "wss://eventsub.wss.twitch.tv/ws",
	})
	if err != nil {
		log.Fatal(err)
	}

	conn.ReadLoop()
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
