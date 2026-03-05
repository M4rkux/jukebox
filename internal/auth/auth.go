package auth

import (
	"context"
	"encoding/gob"
	"net/http"

	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/spotify"
)

const (
	SessionName     = "jukebox_session"
	SessionUserKey  = "user_id"
	SessionTokenKey = "access_token"
)

func init() {
	gob.Register(map[string]interface{}{})
}

// NewSpotifyOAuth creates the OAuth2 config for Spotify
func NewSpotifyOAuth(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes: []string{
			"user-read-private",
			"user-read-email",
			"user-read-playback-state",
			"user-modify-playback-state",
			"user-read-currently-playing",
			"playlist-read-private",
			"playlist-read-collaborative",
		},
		Endpoint: spotify.Endpoint,
	}
}

// SessionStore wraps gorilla sessions
type SessionStore struct {
	store *sessions.CookieStore
}

func NewSessionStore(secret string) *SessionStore {
	store := sessions.NewCookieStore([]byte(secret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 30,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Domain:   "",
	}
	return &SessionStore{store: store}
}

func (s *SessionStore) Get(r *http.Request) (*sessions.Session, error) {
	return s.store.Get(r, SessionName)
}

func (s *SessionStore) GetUserID(r *http.Request) (string, bool) {
	sess, err := s.store.Get(r, SessionName)
	if err != nil {
		return "", false
	}
	uid, ok := sess.Values[SessionUserKey].(string)
	return uid, ok && uid != ""
}

func (s *SessionStore) SetUserID(w http.ResponseWriter, r *http.Request, userID string) error {
	sess, err := s.store.Get(r, SessionName)
	if err != nil {
		return err
	}
	sess.Values[SessionUserKey] = userID
	return sess.Save(r, w)
}

func (s *SessionStore) Clear(w http.ResponseWriter, r *http.Request) error {
	sess, err := s.store.Get(r, SessionName)
	if err != nil {
		return err
	}
	sess.Options.MaxAge = -1
	return sess.Save(r, w)
}

// GuestSessionKey returns a cookie name for a guest on a specific owner's page
func GuestSessionKey(ownerID string) string {
	return "jukebox_guest_" + ownerID
}

func GetGuestSession(store *SessionStore, r *http.Request, ownerID string) (*sessions.Session, error) {
	return store.store.Get(r, GuestSessionKey(ownerID))
}

// Exchange wraps the OAuth token exchange
func Exchange(cfg *oauth2.Config, code string) (*oauth2.Token, error) {
	return cfg.Exchange(context.Background(), code)
}
