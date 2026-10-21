package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"kobo-dashboard/src/internal/model"
)

type Google struct {
	Client        *http.Client
	Credentials   Credentials
	BaseURL       string
	TokenEndpoint string
	mu            sync.Mutex
	token         string
	expires       time.Time
}

func NewGoogle(client *http.Client, c Credentials) *Google {
	return &Google{Client: safeClient(client), Credentials: c, BaseURL: "https://www.googleapis.com/calendar/v3", TokenEndpoint: TokenURL}
}
func (g *Google) accessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.token != "" && time.Now().Add(time.Minute).Before(g.expires) {
		return g.token, nil
	}
	if g.Credentials.ClientID == "" || g.Credentials.RefreshToken == "" {
		return "", ErrAuthorization
	}
	token, err := Exchange(ctx, g.Client, g.TokenEndpoint, url.Values{"client_id": {g.Credentials.ClientID}, "client_secret": {g.Credentials.ClientSecret}, "refresh_token": {g.Credentials.RefreshToken}, "grant_type": {"refresh_token"}})
	if err != nil {
		return "", err
	}
	g.token = token.AccessToken
	g.expires = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return g.token, nil
}
func (g *Google) get(ctx context.Context, path string, q url.Values, out any) error {
	token, err := g.accessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(g.BaseURL, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return errors.New("invalid Google request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := safeClient(g.Client).Do(req)
	if err != nil {
		return errors.New("Google Calendar connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		g.mu.Lock()
		g.token = ""
		g.mu.Unlock()
		return ErrAuthorization
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("Google Calendar returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return errors.New("invalid Google Calendar response")
	}
	return nil
}
func (g *Google) Calendars(ctx context.Context) ([]model.Calendar, error) {
	result := []model.Calendar{}
	page := ""
	seen := map[string]bool{}
	for {
		var response struct {
			Items []model.Calendar `json:"items"`
			Next  string           `json:"nextPageToken"`
		}
		q := url.Values{"maxResults": {"250"}, "fields": {"items(id,summary),nextPageToken"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		if err := g.get(ctx, "/users/me/calendarList", q, &response); err != nil {
			return nil, err
		}
		result = append(result, response.Items...)
		if response.Next == "" {
			return result, nil
		}
		if seen[response.Next] || len(result) > 10000 {
			return nil, errors.New("Google Calendar pagination limit reached")
		}
		seen[response.Next] = true
		page = response.Next
	}
}
func (g *Google) Events(ctx context.Context, ids []string, from, to time.Time, timezone string) ([]model.Event, error) {
	if !to.After(from) {
		return nil, errors.New("calendar range must end after it begins")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, errors.New("invalid calendar timezone")
	}
	result := []model.Event{}
	unique := map[string]bool{}
	for _, id := range ids {
		if id == "" || unique[id] {
			continue
		}
		unique[id] = true
		page := ""
		seen := map[string]bool{}
		for {
			var response struct {
				Items []struct {
					ID      string `json:"id"`
					Summary string `json:"summary"`
					Status  string `json:"status"`
					Start   struct {
						Date     string `json:"date"`
						DateTime string `json:"dateTime"`
					} `json:"start"`
					End struct {
						Date     string `json:"date"`
						DateTime string `json:"dateTime"`
					} `json:"end"`
				} `json:"items"`
				Next string `json:"nextPageToken"`
			}
			q := url.Values{"singleEvents": {"true"}, "orderBy": {"startTime"}, "showDeleted": {"false"}, "maxResults": {"2500"}, "timeMin": {from.Format(time.RFC3339)}, "timeMax": {to.Format(time.RFC3339)}, "timeZone": {timezone}, "fields": {"items(id,summary,status,start,end),nextPageToken"}}
			if page != "" {
				q.Set("pageToken", page)
			}
			if err := g.get(ctx, "/calendars/"+url.PathEscape(id)+"/events", q, &response); err != nil {
				return nil, err
			}
			for _, e := range response.Items {
				if e.Status == "cancelled" {
					continue
				}
				event := model.Event{ID: e.ID, CalendarID: id, Title: e.Summary, Start: e.Start.DateTime, End: e.End.DateTime}
				if e.Start.Date != "" {
					event.AllDay = true
					event.Start = e.Start.Date
					event.End = e.End.Date
				}
				if event.Title == "" {
					event.Title = "(Untitled)"
				}
				if event.Start != "" && event.End != "" {
					result = append(result, event)
				}
			}
			if response.Next == "" {
				break
			}
			if seen[response.Next] || len(result) > 100000 {
				return nil, errors.New("Google events pagination limit reached")
			}
			seen[response.Next] = true
			page = response.Next
		}
	}
	return result, nil
}
