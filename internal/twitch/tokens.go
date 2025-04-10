package twitch

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"

	"github.com/antlu/stream-assistant/internal/crypto"
	"github.com/antlu/stream-assistant/internal/interfaces"
)

type tokensData struct {
	AccessToken  string   `json:"access_token"`
	ExpiresIn    int      `json:"expires_in"`
	RefreshToken string   `json:"refresh_token"`
	Scope        []string `json:"scope"`
	TokenType    string   `json:"token_type"`
}

type tokens struct {
	accessToken  string
	refreshToken string
}

type TokenManager struct {
	mu           sync.RWMutex
	cache        map[string]tokens
	store        interfaces.DBQueryExecCloser
	cipher       crypto.Cipher
	refreshQueue map[string]chan (tokens)
}

func NewTokenManager(store interfaces.DBQueryExecCloser, cipher crypto.Cipher) *TokenManager {
	return &TokenManager{
		store:        store,
		cache:        make(map[string]tokens),
		cipher:       cipher,
		refreshQueue: make(map[string]chan (tokens)),
	}
}

func (*TokenManager) refreshTokens(refreshToken string) (string, string, error) {
	resp, err := http.PostForm("https://id.twitch.tv/oauth2/token", url.Values{
		"client_id":     {os.Getenv("SA_CLIENT_ID")},
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

func (tm *TokenManager) getTokens(channelName string) (string, string, bool, error) {
	var accessToken, refreshToken string

	tokenPair, cached := tm.cache[channelName]
	if cached {
		accessToken, refreshToken = tokenPair.accessToken, tokenPair.refreshToken
	} else {
		var err error
		accessToken, refreshToken, err = tm.readFromStore(channelName)
		if err != nil {
			return "", "", false, err
		}
	}

	return accessToken, refreshToken, cached, nil
}

func (tm *TokenManager) updateCache(channelName, accessToken, refreshToken string) {
	tm.mu.Lock()
	tm.cache[channelName] = tokens{accessToken, refreshToken}
	tm.mu.Unlock()
}

func (tm *TokenManager) readFromStore(channelName string) (string, string, error) {
	var accessToken, refreshToken string

	err := tm.store.QueryRow("SELECT access_token, refresh_token FROM channels WHERE login = ?", channelName).Scan(&accessToken, &refreshToken)
	if err != nil {
		return "", "", err
	}

	accessToken, err = tm.cipher.Decrypt(accessToken)
	if err != nil {
		return "", "", err
	}

	refreshToken, err = tm.cipher.Decrypt(refreshToken)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (tm *TokenManager) updateStoreRecord(channelName, accessToken, refreshToken string) error {
	accessToken, err := tm.cipher.Encrypt(accessToken)
	if err != nil {
		return fmt.Errorf("error encrypting access token: %v", err)
	}

	refreshToken, err = tm.cipher.Encrypt(refreshToken)
	if err != nil {
		return fmt.Errorf("error encrypting refresh token: %v", err)
	}

	res, err := tm.store.Exec(
		"UPDATE channels SET access_token = ?, refresh_token = ? WHERE login = ?",
		accessToken, refreshToken, channelName,
	)
	affected, _ := res.RowsAffected()
	if err != nil || affected == 0 {
		return fmt.Errorf("error updating token store: %v", err)
	}

	log.Printf("Updated tokens for %s", channelName)
	return nil
}

func (tm *TokenManager) updateStorage(channelName, accessToken, refreshToken string) error {
	tm.updateCache(channelName, accessToken, refreshToken)
	return tm.updateStoreRecord(channelName, accessToken, refreshToken)
}

func (tm *TokenManager) ensureValidTokens(channelName string) (accessToken, refreshToken string, _ error) {
	tm.mu.Lock()
	tokensCh, exists := tm.refreshQueue[channelName]
	if exists {
		tm.mu.Unlock()
		tokens := <-tokensCh
		tm.mu.Lock()
		delete(tm.refreshQueue, channelName)
		tm.mu.Unlock()
		return tokens.accessToken, tokens.refreshToken, nil
	}

	tokensCh = make(chan tokens, 1)
	tm.refreshQueue[channelName] = tokensCh
	tm.mu.Unlock()
	defer func() {
		tokensCh <- tokens{accessToken, refreshToken}
	}()

	accessToken, refreshToken, cached, err := tm.getTokens(channelName)
	if err != nil {
		return "", "", fmt.Errorf("error getting access token: %w", err)
	}

	isTokenValid, err := validateToken(accessToken)
	if err != nil {
		return "", "", fmt.Errorf("error validating token: %w", err)
	}
	if isTokenValid {
		if !cached {
			tm.updateCache(channelName, accessToken, refreshToken)
		}
		return accessToken, refreshToken, nil
	}

	accessToken, refreshToken, err = tm.refreshTokens(refreshToken)
	if err != nil {
		return "", "", fmt.Errorf("error refreshing token: %w", err)
	}

	err = tm.updateStorage(channelName, accessToken, refreshToken)
	if err != nil {
		return "", "", fmt.Errorf("error updating token store: %w", err)
	}

	return accessToken, refreshToken, nil
}

func (tm *TokenManager) CreateOrUpdateStoreRecord(id, login, accessToken, refreshToken string) error {
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

	var exists bool
	err = tm.store.QueryRow("SELECT EXISTS (SELECT 1 FROM channels WHERE id = ?)", id).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		_, err = tm.store.Exec(
			`INSERT INTO channels (id, login, access_token, refresh_token) VALUES (?, ?, ?, ?)`,
			id, login, accessToken, refreshToken,
		)
		if err != nil {
			return err
		}
	} else {
		_, err = tm.store.Exec(
			"UPDATE channels SET login = ?, access_token = ?, refresh_token = ? WHERE id = ?",
			login, accessToken, refreshToken, id,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func ExchangeCodeForTokens(code string) (*tokensData, error) {
	resp, err := http.PostForm("https://id.twitch.tv/oauth2/token", url.Values{
		"client_id":     {os.Getenv("SA_CLIENT_ID")},
		"client_secret": {os.Getenv("SA_CLIENT_SECRET")},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {os.Getenv("SA_REDIRECT_URI")},
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

func validateToken(accessToken string) (bool, error) {
	req, err := http.NewRequest(http.MethodHead, "https://id.twitch.tv/oauth2/validate", nil)
	if err != nil {
		return false, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("OAuth %s", accessToken))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}

	return resp.StatusCode == http.StatusOK, nil
}
