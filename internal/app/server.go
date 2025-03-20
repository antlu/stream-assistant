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

func StartWebServer(tokenManager *twitch.TokenManager) {
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

		// TODO:DELETE !=
		if r.URL.Query().Get("state") != session.Values["state"] {
			http.Error(w, fmt.Sprintf("Invalid state %s != %s", session.Values["state"], r.URL.Query().Get("state")), http.StatusBadRequest)
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

		go tokenManager.CreateOrUpdateStoreRecord(tokensData)

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
