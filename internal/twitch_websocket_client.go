package internal

import (
	"log"

	"github.com/lxzan/gws"
)

type handler struct {}

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
	log.Printf("recv: %s\n", message.Data.String())
	message.Close()
}

func StartTwitchWSCommunication() {
	conn, _, err := gws.NewClient(handler{}, &gws.ClientOption{
		Addr: "wss://eventsub.wss.twitch.tv/ws",
	})
	if err != nil {
		log.Fatal(err)
	}

	conn.ReadLoop()
}
