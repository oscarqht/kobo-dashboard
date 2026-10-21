package providers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const CalendarScope = "https://www.googleapis.com/auth/calendar.readonly"
const AuthURL = "https://accounts.google.com/o/oauth2/v2/auth"
const TokenURL = "https://oauth2.googleapis.com/token"

// Credentials is the private USB-import file. Never include it in configuration APIs.
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

var ErrAuthorization = errors.New("Google authorization expired or was revoked; reconnect using the setup helper")

func RandomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func AuthorizationURL(clientID, redirect, state, verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {CalendarScope}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "access_type": {"offline"}, "prompt": {"consent"}}
	return AuthURL + "?" + q.Encode()
}

func Exchange(ctx context.Context, client *http.Client, endpoint string, values url.Values) (Token, error) {
	var token Token
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return token, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := safeClient(client).Do(req)
	if err != nil {
		return token, errors.New("Google token request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var result struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result)
		if result.Error == "invalid_grant" {
			return token, ErrAuthorization
		}
		return token, fmt.Errorf("Google token request returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil {
		return token, errors.New("invalid Google token response")
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 {
		return token, errors.New("incomplete Google token response")
	}
	return token, nil
}

func ReadCredentials(path string) (Credentials, error) {
	var c Credentials
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	if err = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&c); err != nil {
		return c, errors.New("invalid credentials file")
	}
	if c.ClientID == "" || c.RefreshToken == "" {
		return c, errors.New("credentials file needs client_id and refresh_token")
	}
	return c, nil
}

func WriteCredentials(path string, c Credentials) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

func safeClient(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copy.Timeout == 0 {
		copy.Timeout = 30 * time.Second
	}
	return &copy
}
