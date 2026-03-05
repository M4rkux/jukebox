package store

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/yourusername/jukebox/internal/models"
)

// Store defines the interface for data persistence
type Store interface {
	// Users
	GetUser(id string) (*models.User, error)
	SaveUser(user *models.User) error

	// Share pages
	GetSharePage(ownerID string) (*models.SharePage, error)
	SaveSharePage(page *models.SharePage) error

	// Guest sessions
	GetGuestSession(sessionID string) (*models.GuestSession, error)
	SaveGuestSession(session *models.GuestSession) error
	GetGuestSessionsByOwner(ownerID, sessionID string) (*models.GuestSession, error)

	// Rate limiting helpers
	CountRecentAdditions(ownerID, guestSessionID string, since time.Time) (int, error)
}

// MemoryStore is an in-memory implementation (replace with Redis/Postgres for production)
type MemoryStore struct {
	mu            sync.RWMutex
	users         map[string]*models.User
	sharePages    map[string]*models.SharePage
	guestSessions map[string]*models.GuestSession
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:         make(map[string]*models.User),
		sharePages:    make(map[string]*models.SharePage),
		guestSessions: make(map[string]*models.GuestSession),
	}
}

func (m *MemoryStore) GetUser(id string) (*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, fmt.Errorf("user not found")
	}
	cp := *u
	return &cp, nil
}

func (m *MemoryStore) SaveUser(user *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *user
	m.users[user.SpotifyID] = &cp
	return nil
}

func (m *MemoryStore) GetSharePage(ownerID string) (*models.SharePage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.sharePages[ownerID]
	if !ok {
		return nil, fmt.Errorf("share page not found")
	}
	cp := *p
	return &cp, nil
}

func (m *MemoryStore) SaveSharePage(page *models.SharePage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *page
	m.sharePages[page.OwnerID] = &cp
	return nil
}

func (m *MemoryStore) GetGuestSession(sessionID string) (*models.GuestSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.guestSessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("guest session not found")
	}
	cp := *s
	return &cp, nil
}

func (m *MemoryStore) GetGuestSessionsByOwner(ownerID, sessionID string) (*models.GuestSession, error) {
	key := ownerID + ":" + sessionID
	return m.GetGuestSession(key)
}

func (m *MemoryStore) SaveGuestSession(session *models.GuestSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := session.OwnerID + ":" + session.SessionID
	cp := *session
	m.guestSessions[key] = &cp
	return nil
}

func (m *MemoryStore) CountRecentAdditions(ownerID, guestSessionID string, since time.Time) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := ownerID + ":" + guestSessionID
	s, ok := m.guestSessions[key]
	if !ok {
		return 0, nil
	}
	count := 0
	for _, t := range s.AddedAt {
		if t.After(since) {
			count++
		}
	}
	return count, nil
}

// GeneratePasscode returns a 6-digit numeric passcode
func GeneratePasscode() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	n := int(b[0])<<16 | int(b[1])<<8 | int(b[2])
	return fmt.Sprintf("%06d", n%1000000), nil
}

// GenerateSessionID returns a random hex session ID
func GenerateSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}
