package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const baseURL = "https://api.spotify.com/v1"

// Client wraps the Spotify Web API
type Client struct {
	httpClient *http.Client
	token      *oauth2.Token
	oauthCfg   *oauth2.Config
}

func New(token *oauth2.Token, cfg *oauth2.Config) *Client {
	tokenSource := cfg.TokenSource(context.Background(), token)
	return &Client{
		httpClient: oauth2.NewClient(context.Background(), tokenSource),
		token:      token,
		oauthCfg:   cfg,
	}
}

func (c *Client) do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// Profile fetches the current user's Spotify profile
type ProfileResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Images      []struct {
		URL string `json:"url"`
	} `json:"images"`
}

func (c *Client) GetProfile() (*ProfileResponse, error) {
	resp, err := c.do("GET", "/me", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var p ProfileResponse
	return &p, json.NewDecoder(resp.Body).Decode(&p)
}

// NowPlaying fetches current playback
type CurrentlyPlayingResponse struct {
	IsPlaying bool `json:"is_playing"`
	ProgressMs int `json:"progress_ms"`
	Item      *TrackObject `json:"item"`
}

type TrackObject struct {
	ID      string `json:"id"`
	URI     string `json:"uri"`
	Name    string `json:"name"`
	Explicit bool  `json:"explicit"`
	DurationMs int `json:"duration_ms"`
	PreviewURL string `json:"preview_url"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name   string `json:"name"`
		Images []struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"images"`
		ReleaseDate string `json:"release_date"`
	} `json:"album"`
}

func (c *Client) GetCurrentlyPlaying() (*CurrentlyPlayingResponse, error) {
	resp, err := c.do("GET", "/me/player/currently-playing", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 204 {
		return &CurrentlyPlayingResponse{}, nil
	}
	defer resp.Body.Close()
	var cp CurrentlyPlayingResponse
	return &cp, json.NewDecoder(resp.Body).Decode(&cp)
}

// Queue fetches the user's current queue
type QueueResponse struct {
	CurrentlyPlaying *TrackObject  `json:"currently_playing"`
	Queue            []TrackObject `json:"queue"`
}

func (c *Client) GetQueue() (*QueueResponse, error) {
	resp, err := c.do("GET", "/me/player/queue", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 204 || resp.StatusCode == 404 {
		return &QueueResponse{}, nil
	}
	defer resp.Body.Close()
	var q QueueResponse
	return &q, json.NewDecoder(resp.Body).Decode(&q)
}

// AddToQueue adds a track URI to the queue
func (c *Client) AddToQueue(trackURI string) error {
	path := fmt.Sprintf("/me/player/queue?uri=%s", url.QueryEscape(trackURI))
	resp, err := c.do("POST", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("spotify error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// Search searches for tracks
type SearchResponse struct {
	Tracks struct {
		Items []TrackObject `json:"items"`
	} `json:"tracks"`
}

func (c *Client) SearchTracks(q string, limit int) (*SearchResponse, error) {
	path := fmt.Sprintf("/search?q=%s&type=track&limit=%d", url.QueryEscape(q), limit)
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var sr SearchResponse
	return &sr, json.NewDecoder(resp.Body).Decode(&sr)
}

// GetAudioFeatures fetches audio features for genre filtering
type AudioFeatures struct {
	ID  string  `json:"id"`
}

// GetTrackGenres attempts to get genres via the track's artist
func (c *Client) GetArtistGenres(artistID string) ([]string, error) {
	resp, err := c.do("GET", "/artists/"+artistID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var ar struct {
		Genres []string `json:"genres"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, err
	}
	return ar.Genres, nil
}

// GetUserPlaylists fetches the user's playlists
type PlaylistsResponse struct {
	Items []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"items"`
}

func (c *Client) GetUserPlaylists() (*PlaylistsResponse, error) {
	resp, err := c.do("GET", "/me/playlists?limit=50", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var pr PlaylistsResponse
	return &pr, json.NewDecoder(resp.Body).Decode(&pr)
}

// GetPlaylistTracks fetches tracks from a playlist
func (c *Client) GetPlaylistTrackIDs(playlistID string) (map[string]bool, error) {
	ids := make(map[string]bool)
	offset := 0
	for {
		path := fmt.Sprintf("/playlists/%s/tracks?limit=100&offset=%d&fields=items(track(id)),next", playlistID, offset)
		resp, err := c.do("GET", path, nil)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var pr struct {
			Items []struct {
				Track struct {
					ID string `json:"id"`
				} `json:"track"`
			} `json:"items"`
			Next string `json:"next"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
			return nil, err
		}
		for _, item := range pr.Items {
			ids[item.Track.ID] = true
		}
		if pr.Next == "" {
			break
		}
		offset += 100
	}
	return ids, nil
}

// Helper: get best image URL (prefer ~300px)
func BestImage(images []struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}) string {
	if len(images) == 0 {
		return ""
	}
	best := images[0].URL
	for _, img := range images {
		if img.Width >= 200 && img.Width <= 400 {
			return img.URL
		}
	}
	return best
}

func TrackYear(releaseDate string) string {
	if len(releaseDate) >= 4 {
		return releaseDate[:4]
	}
	return ""
}

func TrackDecade(year string) string {
	if len(year) < 4 {
		return ""
	}
	y := year[:3]
	return y + "0s"
}

// TokenExpired checks if the token needs refresh
func TokenExpired(t *oauth2.Token) bool {
	return time.Now().After(t.Expiry.Add(-5 * time.Minute))
}

// BuildSearchWithFilters adds genre/year info to a query
func BuildFilteredQuery(query string, genres []string, decades []string) string {
	q := query
	if len(genres) > 0 {
		q += " genre:" + strings.Join(genres, " genre:")
	}
	if len(decades) > 0 {
		// Spotify doesn't support decade filter natively, handled post-search
	}
	return q
}
