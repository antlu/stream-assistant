package app

import (
	"crypto/rand"
	"encoding/base64"
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
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		log.Fatal(err)
	}

	cookieStore := sessions.NewCookieStore([]byte(os.Getenv("SA_SECURE_KEY")))

	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		session, err := cookieStore.Get(r, "sa_session")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		session.Options.MaxAge = 0
		session.Options.SameSite = http.SameSiteLaxMode

		twitchAuthQueryParams := prepareTwitchAuthQueryParams()
		session.Values["state"] = twitchAuthQueryParams.Get("state")
		flashes := session.Flashes()
		err = session.Save(r, w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl.Execute(w, map[string]any{
			"flashes":      flashes,
			"twitchParams": twitchAuthQueryParams,
		})
	})

	mux.HandleFunc("GET /auth", func(w http.ResponseWriter, r *http.Request) {
		session, err := cookieStore.Get(r, "sa_session")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		go func() {
			apiClient, err := helix.NewClient(&helix.Options{
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
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	go func() {
		log.Print("Server is listening on port 3000")
		log.Print(http.ListenAndServe(":3000", mux))
	}()
}
