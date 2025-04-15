package app

import (
	"fmt"
	"log"
	"maps"
	"math/rand"
	"net/http"
	"slices"

	"github.com/antlu/stream-assistant/internal/interfaces"
	"github.com/nicklaw5/helix/v2"
)

type RaffleManager struct {
	DB interfaces.DBQueryExecCloser
}

func (rm RaffleManager) PickWinner(channel *Channel) (string, error) {
	channel.Raffle.IsActive = false

	participantIDs := slices.Collect(maps.Keys(channel.Raffle.Participants))

	rand.Shuffle(len(participantIDs), func(i, j int) {
		participantIDs[i], participantIDs[j] = participantIDs[j], participantIDs[i]
	})

	vips, err := channel.ApiClient.GetChannelVips(channel.ID)
	if err != nil {
		return "", err
	}

	vipIDs := make([]string, 0, len(vips))
	for _, vip := range vips {
		vipIDs = append(vipIDs, vip.UserID)
	}

	var (
		loser  RaffleParticipant
		winner RaffleParticipant
	)

	for _, participantID := range participantIDs {
		if !slices.Contains(vipIDs, participantID) {
			winner = channel.Raffle.Participants[participantID]
			break
		}
		if loser.ID == "" {
			loser = channel.Raffle.Participants[participantID]
		}
	}

	if winner.ID == "" {
		return "No one has won", err
	}

	for i := 0; i < 2; i++ {
		log.Printf("VIPs routine: attempt %d", i+1)

		if loser.ID != "" {
			_, err := channel.ApiClient.RemoveChannelVip(&helix.RemoveChannelVipParams{
				UserID:        loser.ID,
				BroadcasterID: channel.ID,
			})
			if err != nil {
				log.Print(err)
			}

			log.Printf("Demoted %s", loser.Name)
		}

		resp, err := channel.ApiClient.AddChannelVip(&helix.AddChannelVipParams{
			UserID:        winner.ID,
			BroadcasterID: channel.ID,
		})
		if err != nil {
			log.Print(err)
		}
		if resp.StatusCode == http.StatusNoContent {
			log.Printf("Promoted %s", winner.Name)
			break
		}
		if resp.StatusCode == http.StatusConflict {
			log.Print("No free slots. Will search who to demote")
			err = rm.DB.QueryRow(`
				SELECT viewer_id, username
				FROM channel_viewers JOIN viewers ON viewer_id = id
				WHERE channel_id = ?
				ORDER BY datetime(last_seen) ASC NULLS FIRST LIMIT 1
			`, channel.ID).Scan(&loser.ID, &loser.Name)
			if err != nil {
				log.Print(err)
			}
		}
	}

	unvipMsg := ""
	if loser.ID != "" {
		unvipMsg = fmt.Sprintf("%s has lost their status. ", loser.Name)
	}

	return fmt.Sprintf("%sNew VIP — %s!", unvipMsg, winner.Name), nil
}
