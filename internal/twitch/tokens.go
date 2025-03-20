package twitch

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/antlu/stream-assistant/internal/crypto"
)

type tokensData struct {
	AccessToken  string   `json:"access_token"`
	ExpiresIn    int      `json:"expires_in"`
	RefreshToken string   `json:"refresh_token"`
	Scope        []string `json:"scope"`
	TokenType    string   `json:"token_type"`
}

type tokens struct {
	accessToken string
	refreshToken string
}

type TokenManager struct {
	sync.RWMutex
	cache  map[string]tokens
	store  *sql.DB
	cipher crypto.Cipher
}

func NewTokenManager(store *sql.DB, cipher crypto.Cipher) *TokenManager {
	return &TokenManager{
		store:  store,
		cipher: cipher,
	}
}

func (*TokenManager) refreshTokens(refreshToken string) (string, string, error) {
	resp, err := http.PostForm("https://id.twitch.tv/oauth2/token", url.Values{
		"client_id":     {"jmaoofuyr1c4v8lqzdejzfppdj5zym"},
		"client_secret": {os.Getenv("SA_CLIENT_SECRET")},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var tokensData tokensData
	err = json.NewDecoder(resp.Body).Decode(&tokensData)
	if err != nil {
		return "", "", err
	}

	return tokensData.AccessToken, tokensData.RefreshToken, nil
}

func (tm *TokenManager) getValidAccessToken(channelName string) (string, error) {
	var accessToken, refreshToken string

	tokenPair, cached := tm.cache[channelName]
	if !cached {
		var err error
		accessToken, refreshToken, err = tm.readFromStore(channelName)
		if err != nil {
			return "", err
		}

		tm.RLock()
		tm.cache[channelName] = tokens{accessToken, refreshToken}
		tm.RUnlock()
	}

	accessToken, refreshToken = tokenPair.accessToken, tokenPair.refreshToken
	apiClient, err := NewApiClient(accessToken)
	if err != nil {
		return "", fmt.Errorf("error creating API client: %w", err)
	}

	isTokenValid, _, err := apiClient.ValidateToken(accessToken)
	if err != nil {
		return "", fmt.Errorf("error validating token: %w", err)
	}

	if !isTokenValid {
		accessToken, refreshToken, err = tm.refreshTokens(refreshToken)
		if err != nil {
			return "", fmt.Errorf("error refreshing token: %w", err)
		}
		tm.Lock()
		tm.cache[channelName] = tokens{accessToken, refreshToken}
		tm.Unlock()

		tm.updateStoreRecord(channelName, accessToken, refreshToken)
	}

	return accessToken, nil
}

func (tm *TokenManager) readFromStore(channelName string) (string, string, error) {
	var accessToken, refreshToken string

	for {
		err := tm.store.QueryRow("SELECT access_token, refresh_token FROM channels WHERE login = ?", channelName).Scan(&accessToken, &refreshToken)
		if err == nil {
			accessToken, err = tm.cipher.Decrypt(accessToken)
			if err != nil {
				return "", "", err
			}

			refreshToken, err = tm.cipher.Decrypt(refreshToken)
			if err != nil {
				return "", "", err
			}

			break
		}

		log.Printf("Waiting for %s authorization", channelName)
		time.Sleep(30 * time.Second)
	}

	return accessToken, refreshToken, nil
}

func (tm *TokenManager) updateStoreRecord(channelName, accessToken, refreshToken string) error {
	accessToken, err := tm.cipher.Encrypt(accessToken)
	if err != nil {
		return fmt.Errorf("error encrypting access token: %w", err)
	}

	refreshToken, err = tm.cipher.Encrypt(refreshToken)
	if err != nil {
		return fmt.Errorf("error encrypting refresh token: %w", err)
	}

	res, err := tm.store.Exec(
		"UPDATE channels SET access_token = ?, refresh_token = ? WHERE login = ?",
		accessToken, refreshToken, channelName,
	)
	affected, err := res.RowsAffected()
	if err != nil || affected == 0 {
		return fmt.Errorf("error updating token store: %w", err)
	}

	return nil
}

func (tm *TokenManager) CreateOrUpdateStoreRecord(tokensData *tokensData) {
	apiClient, err := NewApiClient(tokensData.AccessToken)
	if err != nil {
		log.Print(err)
		return
	}
	usersResp, err := apiClient.GetUsers(nil)
	if err != nil {
		log.Print("Error getting user info")
		return
	}

	userData := usersResp.Data.Users[0]
	err = tm.createOrUpdateStoreRecord(userData.ID, userData.Login, tokensData.AccessToken, tokensData.RefreshToken)
	if err != nil {
		log.Print(err)
		return
	}
}

func (tm *TokenManager) createOrUpdateStoreRecord(id, login, accessToken, refreshToken string) error {
	accessToken, err := tm.cipher.Encrypt(accessToken)
	if err != nil {
		log.Print(err)
		return err
	}

	refreshToken, err = tm.cipher.Encrypt(refreshToken)
	if err != nil {
		log.Print(err)
		return err
	}

	// var affected int
	// err = tm.store.QueryRow("SELECT EXISTS (SELECT 1 FROM channels WHERE id = ?)", id).Scan(&affected)
	// if affected == 0 {

	// }
	err = tm.store.QueryRow("SELECT EXISTS (SELECT 1 FROM channels WHERE id = ?)", id).Scan(nil)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tm.store.Exec(
			`INSERT INTO channels (id, login, access_token, refresh_token) VALUES (?, ?, ?, ?)`,
			id, login, accessToken, refreshToken,
		)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	_, err = tm.store.Exec(
		"UPDATE channels SET login = ?, access_token = ?, refresh_token = ? WHERE id = ?",
		login, accessToken, refreshToken, id,
	)
	if err != nil {
		return err
	}

	return nil
}

func ExchangeCodeForTokens(code string) (*tokensData, error) {
	resp, err := http.PostForm("https://id.twitch.tv/oauth2/token", url.Values{
		"client_id":     {"jmaoofuyr1c4v8lqzdejzfppdj5zym"},
		"client_secret": {os.Getenv("SA_CLIENT_SECRET")},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {"http://localhost:3000/auth"},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tokensData tokensData
	err = json.NewDecoder(resp.Body).Decode(&tokensData)
	if err != nil {
		return nil, err
	}

	return &tokensData, nil
}
