package main

import (
	"log"
	"net/http"
	"os"

	"github.com/M4rkux/jukebox/internal/auth"
	"github.com/M4rkux/jukebox/internal/handlers"
	"github.com/M4rkux/jukebox/internal/middleware"
	"github.com/M4rkux/jukebox/internal/store"
)

func main() {
	// Load env / config
	port := getEnv("PORT", "8080")
	clientID := getEnv("SPOTIFY_CLIENT_ID", "")
	clientSecret := getEnv("SPOTIFY_CLIENT_SECRET", "")
	sessionSecret := getEnv("SESSION_SECRET", "super-secret-change-me")
	baseURL := getEnv("BASE_URL", "http://127.0.0.1:"+port)

	if clientID == "" || clientSecret == "" {
		log.Fatal("SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET must be set")
	}

	// Initialize stores
	appStore := store.NewMemoryStore()

	// Initialize OAuth
	oauthCfg := auth.NewSpotifyOAuth(clientID, clientSecret, baseURL+"/auth/callback")

	// Initialize session store
	sessionStore := auth.NewSessionStore(sessionSecret)

	// Build handler
	h := handlers.New(handlers.Config{
		Store:        appStore,
		OAuth:        oauthCfg,
		SessionStore: sessionStore,
		BaseURL:      baseURL,
	})

	mux := http.NewServeMux()

	// Static files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Auth routes
	mux.HandleFunc("/auth/login", h.HandleLogin)
	mux.HandleFunc("/auth/callback", h.HandleCallback)
	mux.HandleFunc("/auth/logout", h.HandleLogout)

	// Main app (requires auth)
	mux.Handle("/dashboard", middleware.RequireAuth(sessionStore, h.HandleDashboard))
	mux.Handle("/dashboard/passcode/toggle", middleware.RequireAuth(sessionStore, h.HandleTogglePasscode))
	mux.Handle("/dashboard/passcode/regenerate", middleware.RequireAuth(sessionStore, h.HandleRegeneratePasscode))
	mux.Handle("/dashboard/limits", middleware.RequireAuth(sessionStore, h.HandleUpdateLimits))
	mux.Handle("/dashboard/qr", middleware.RequireAuth(sessionStore, h.HandleQRCode))

	// Root — index or share page dispatch
	// We use a single catch-all and route manually to avoid wildcard conflicts
	// with fixed prefixes like /static/, /auth/, /dashboard
	mux.Handle("/", middleware.Session(sessionStore, h.HandleRoot))

	log.Printf("🎵 Jukebox running on http://127.0.0.1:%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
