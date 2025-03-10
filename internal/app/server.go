package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/antlu/stream-assistant/internal/twitch"
	"github.com/gorilla/sessions"
)

func generateSecret() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)
}

func prepareTwitchAuthQueryParams() url.Values {
	params := url.Values{}
	params.Add("client_id", "jmaoofuyr1c4v8lqzdejzfppdj5zym")
	params.Add("redirect_uri", "http://localhost:3000/auth")
	params.Add("response_type", "code")
	params.Add("scope", "moderation:read moderator:read:chatters channel:manage:vips chat:edit chat:read")
	params.Add("state", generateSecret())
	return params
}

func StartWebServer() {
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

		twitchAuthQueryParams := prepareTwitchAuthQueryParams()
		session.Values["state"] = twitchAuthQueryParams.Get("state")
		flashes := session.Flashes()
		err = session.Save(r, w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl.Execute(w, map[string]any {
			"flashes": flashes,
			"twitchParams":  twitchAuthQueryParams,
		})
	})

	mux.HandleFunc("GET /auth", func(w http.ResponseWriter, r *http.Request) {
		session, err := cookieStore.Get(r, "sa_session")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		state := r.URL.Query().Get("state")
		if state != session.Values["state"] {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}

		error_ := r.URL.Query().Get("error")
		if error_ != "" {
			http.Error(w, r.URL.Query().Get("error_description"), http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		resp, err := http.PostForm("https://id.twitch.tv/oauth2/token", url.Values{
			"client_id":     {"jmaoofuyr1c4v8lqzdejzfppdj5zym"},
			"client_secret": {os.Getenv("SA_CLIENT_SECRET")},
			"code":          {code},
			"grant_type":    {"authorization_code"},
			"redirect_uri":  {"http://localhost:3000/auth"},
		})
		if err != nil {
			log.Print(err)
		}
		defer resp.Body.Close()

		var tokenResponse struct {
			AccessToken string `json:"access_token"`
			ExpiresIn int `json:"expires_in"`
			RefreshToken string `json:"refresh_token"`
			Scope []string `json:"scope"`
			TokenType string `json:"token_type"`
		}
		err = json.NewDecoder(resp.Body).Decode(&tokenResponse)
		if err != nil {
			log.Print(err)
		}

		go func() {
			apiClient := twitch.NewApiClient(tokenResponse.AccessToken)
			usersResp, err := apiClient.GetUsers(nil)
			if err != nil {
				log.Print("Error getting user info")
				return
			}
			userData := usersResp.Data.Users[0]

			db := openDB()
			defer db.Close()
			err = db.QueryRow("SELECT id FROM users WHERE id = ?", userData.ID).Scan(&userData.ID)
			if errors.Is(err, sql.ErrNoRows) {
				_, err = db.Exec(
					`INSERT INTO users (id, login, access_token, refresh_token) VALUES (?, ?, ?, ?)`,
					userData.ID, userData.Login, tokenResponse.AccessToken, tokenResponse.RefreshToken,
				)
				if err != nil {
					log.Print(err)
				}
			} else if err != nil {
				log.Print(err)
				return
			}
			_, err = db.Exec(
				"UPDATE users SET access_token = ?, refresh_token = ? WHERE id = ?",
				tokenResponse.AccessToken, tokenResponse.RefreshToken, userData.ID,
			)
			if err != nil {
				log.Print(err)
			}
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
