package app

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/antlu/stream-assistant/internal/twitch"
	"github.com/gorilla/sessions"
	"github.com/nicklaw5/helix/v2"
)

func generateSecret() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)
}

func prepareTwitchAuthQueryParams() url.Values {
	params := url.Values{}
	params.Add("client_id", os.Getenv("SA_CLIENT_ID"))
	params.Add("redirect_uri", os.Getenv("SA_REDIRECT_URI"))
	params.Add("response_type", "code")
	params.Add("scope", "moderation:read moderator:read:chatters channel:manage:vips chat:edit chat:read")
	params.Add("state", generateSecret())
	return params
}

func StartWebServer(app *App, tokenManager *twitch.TokenManager) {
	cookieStore := sessions.NewCookieStore([]byte(os.Getenv("SA_SECURE_KEY")))

	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		session, err := cookieStore.Get(r, "sa_session")
		if respondWithError(w, err, http.StatusInternalServerError) {
			return
		}
		session.Options.MaxAge = 0
		session.Options.SameSite = http.SameSiteLaxMode

		twitchAuthQueryParams := prepareTwitchAuthQueryParams()
		session.Values["state"] = twitchAuthQueryParams.Get("state")
		flashes := session.Flashes()
		err = session.Save(r, w)
		if respondWithError(w, err, http.StatusInternalServerError) {
			return
		}

		renderTemplate(w, "index", map[string]any{
			"flashes":      flashes,
			"twitchParams": twitchAuthQueryParams,
		})
	})

	mux.HandleFunc("GET /auth", func(w http.ResponseWriter, r *http.Request) {
		session, err := cookieStore.Get(r, "sa_session")
		if respondWithError(w, err, http.StatusInternalServerError) {
			return
		}

		if r.URL.Query().Get("state") != session.Values["state"] {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		error_ := r.URL.Query().Get("error")
		if error_ != "" {
			http.Error(w, r.URL.Query().Get("error_description"), http.StatusBadRequest)
			return
		}

		tokensData, err := twitch.ExchangeCodeForTokens(r.URL.Query().Get("code"))
		if respondWithError(w, err, http.StatusInternalServerError) {
			return
		}

		go func() {
			apiClient, err := helix.NewClient(&helix.Options{
				APIBaseURL:      os.Getenv("SA_TWITCH_API_BASE_URL"),
				ClientID:        os.Getenv("SA_CLIENT_ID"),
				ClientSecret:    os.Getenv("SA_CLIENT_SECRET"),
				UserAccessToken: tokensData.AccessToken,
			})
			if err != nil {
				log.Print("Error creating a one-time API client")
				return
			}

			usersResp, err := apiClient.GetUsers(nil)
			if err != nil {
				log.Print("Error getting user info")
				return
			}

			userData := usersResp.Data.Users[0]

			err = tokenManager.CreateOrUpdateStoreRecord(userData.ID, userData.Login, tokensData.AccessToken, tokensData.RefreshToken)
			if err != nil {
				log.Print(err)
				return
			}

			err = app.addChannel(userData.ID, userData.Login)
			if err != nil {
				log.Print(err)
				return
			}

			app.ircClient.Join(userData.Login)
		}()

		session.AddFlash("Authorized")
		err = session.Save(r, w)
		if respondWithError(w, err, http.StatusInternalServerError) {
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("GET /channels/{channel_name}/vips", func(w http.ResponseWriter, r *http.Request) {
		channelName := r.PathValue("channel_name")
		rows, err := app.db.Query(
			`SELECT v.username, cv.last_seen
			FROM channels AS c
			JOIN channel_viewers AS cv ON c.id = cv.channel_id
			JOIN viewers AS v ON cv.viewer_id = v.id
			WHERE c.login = ?
			ORDER BY datetime(cv.last_seen) ASC NULLS FIRST`,
			channelName,
		)
		if respondWithError(w, err, http.StatusInternalServerError) {
			return
		}

		vips := []channelVip{}
		for rows.Next() {
			vip := channelVip{ChannelName: channelName}
			err = rows.Scan(&vip.Username, &vip.LastSeen)
			if respondWithError(w, err, http.StatusInternalServerError) {
				return
			}
			vips = append(vips, vip)
		}
		rows.Close()
		err = rows.Err()
		if respondWithError(w, err, http.StatusInternalServerError) {
			return
		}

		renderTemplate(w, "vips", map[string]any{"channelName": channelName, "vips": vips})
	})

	go func() {
		log.Print("Server is listening on port 3000")
		log.Print(http.ListenAndServe(":3000", mux))
	}()
}

func renderTemplate(w http.ResponseWriter, page string, data any) error {
	tmpl, err := template.ParseFiles(
		"templates/base.html",
		fmt.Sprintf("templates/%s.html", page),
	)
	if err != nil {
		return fmt.Errorf("parsing templates: %v", err)
	}

	return tmpl.Execute(w, data)
}

func respondWithError(w http.ResponseWriter, err error, status int) bool {
	if err != nil {
		http.Error(w, err.Error(), status)
		return true
	}
	return false
}
