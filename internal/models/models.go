package models

import "time"

// User represents an authenticated Spotify user
type User struct {
	SpotifyID   string    `json:"spotify_id"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	ImageURL    string    `json:"image_url"`
	AccessToken string    `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// SharePage holds the configuration for a user's jukebox share page
type SharePage struct {
	OwnerID     string    `json:"owner_id"`
	Passcode    string    `json:"passcode"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Limits      Limits    `json:"limits"`
}

// Limits controls what guests can add to the queue
type Limits struct {
	Enabled         bool     `json:"enabled"`
	SongsPerWindow  int      `json:"songs_per_window"`   // how many songs
	WindowMinutes   int      `json:"window_minutes"`     // in how many minutes
	AllowedGenres   []string `json:"allowed_genres"`
	AllowedDecades  []string `json:"allowed_decades"`    // e.g. "1980s", "1990s"
	AllowedPlaylists []string `json:"allowed_playlists"` // Spotify playlist IDs
	MaxDurationSec  int      `json:"max_duration_sec"`   // 0 = unlimited
	ExplicitAllowed bool     `json:"explicit_allowed"`
}

// GuestSession tracks a guest's session on a share page
type GuestSession struct {
	SessionID   string    `json:"session_id"`
	OwnerID     string    `json:"owner_id"`
	AddedTracks []string  `json:"added_tracks"` // Spotify track URIs added by this guest
	AddedAt     []time.Time `json:"added_at"`
}

// Track is a simplified Spotify track
type Track struct {
	URI        string   `json:"uri"`
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album"`
	AlbumImage string   `json:"album_image"`
	DurationMs int      `json:"duration_ms"`
	Explicit   bool     `json:"explicit"`
	PreviewURL string   `json:"preview_url"`
	Year       string   `json:"year"`
}

// NowPlaying holds the current playback state
type NowPlaying struct {
	Track      *Track  `json:"track"`
	IsPlaying  bool    `json:"is_playing"`
	ProgressMs int     `json:"progress_ms"`
}

// QueueItem wraps a track with who added it
type QueueItem struct {
	Track     Track  `json:"track"`
	AddedBy   string `json:"added_by"` // guest session ID or "owner"
	QueuedAt  string `json:"queued_at"`
}
