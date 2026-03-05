package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/oauth2"

	"github.com/M4rkux/jukebox/internal/auth"
	"github.com/M4rkux/jukebox/internal/models"
	spotifyclient "github.com/M4rkux/jukebox/internal/spotify"
	"github.com/M4rkux/jukebox/internal/store"
)

// Config holds handler dependencies
type Config struct {
	Store        store.Store
	OAuth        *oauth2.Config
	SessionStore *auth.SessionStore
	BaseURL      string
}

// Handler holds all HTTP handlers
type Handler struct {
	cfg       Config
	templates map[string]*template.Template
}

func New(cfg Config) *Handler {
	h := &Handler{cfg: cfg}
	h.loadTemplates()
	return h
}

func (h *Handler) loadTemplates() {
	h.templates = make(map[string]*template.Template)

	funcMap := template.FuncMap{
		"join": strings.Join,
		"msToMin": func(ms int) string {
			s := ms / 1000
			return fmt.Sprintf("%d:%02d", s/60, s%60)
		},
		"add": func(a, b int) int { return a + b },
	}

	pages := []string{"index", "dashboard", "share", "passcode_prompt"}
	for _, page := range pages {
		pattern := filepath.Join("templates", "pages", page+".html")
		layout := filepath.Join("templates", "layouts", "base.html")
		tmpl, err := template.New(page).Funcs(funcMap).ParseFiles(layout, pattern)
		if err != nil {
			// Also try loading components
			_ = err
			tmpl, _ = template.New(page).Funcs(funcMap).ParseGlob(filepath.Join("templates", "**", "*.html"))
		}
		h.templates[page] = tmpl
	}

	// Also parse component-only templates
	for _, comp := range []string{"now_playing", "queue", "search_results", "track_item"} {
		compPath := filepath.Join("templates", "components", comp+".html")
		tmpl, _ := template.New(comp).Funcs(funcMap).ParseFiles(compPath)
		h.templates[comp] = tmpl
	}
}

func (h *Handler) render(w http.ResponseWriter, name string, data interface{}) {
	tmpl, ok := h.templates[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) renderPartial(w http.ResponseWriter, name string, data interface{}) {
	tmpl, ok := h.templates[name]
	if !ok {
		http.Error(w, "partial not found: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state := "random-state-" + strconv.FormatInt(time.Now().Unix(), 10)
	sess, _ := h.cfg.SessionStore.Get(r)
	sess.Values["oauth_state"] = state
	sess.Save(r, w)
	http.Redirect(w, r, h.cfg.OAuth.AuthCodeURL(state), http.StatusSeeOther)
}

func (h *Handler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	sess, _ := h.cfg.SessionStore.Get(r)
	expectedState, _ := sess.Values["oauth_state"].(string)
	if r.URL.Query().Get("state") != expectedState {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	token, err := auth.Exchange(h.cfg.OAuth, r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	client := spotifyclient.New(token, h.cfg.OAuth)
	profile, err := client.GetProfile()
	if err != nil {
		http.Error(w, "profile fetch failed", http.StatusInternalServerError)
		return
	}

	imageURL := ""
	if len(profile.Images) > 0 {
		imageURL = profile.Images[0].URL
	}

	user := &models.User{
		SpotifyID:    profile.ID,
		DisplayName:  profile.DisplayName,
		Email:        profile.Email,
		ImageURL:     imageURL,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.Expiry,
		CreatedAt:    time.Now(),
	}
	h.cfg.Store.SaveUser(user)

	// Create share page if not exists
	if _, err := h.cfg.Store.GetSharePage(profile.ID); err != nil {
		passcode, _ := store.GeneratePasscode()
		h.cfg.Store.SaveSharePage(&models.SharePage{
			OwnerID:   profile.ID,
			Passcode:  passcode,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Limits: models.Limits{
				ExplicitAllowed: true,
			},
		})
	}

	h.cfg.SessionStore.SetUserID(w, r, profile.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	h.cfg.SessionStore.Clear(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// HandleRoot dispatches "/" (index) and "/{username}/..." (share pages).
// This avoids wildcard conflicts with /static/, /auth/, /dashboard in Go 1.22's mux.
func (h *Handler) HandleRoot(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Exact root → index / dashboard redirect
	if path == "/" {
		h.HandleIndex(w, r)
		return
	}

	// Split into parts: ["", "username"] or ["", "username", "sub"]
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
	username := parts[0]
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}

	if username == "" {
		http.NotFound(w, r)
		return
	}

	switch sub {
	case "":
		h.HandleSharePage(w, r, username)
	case "verify":
		h.HandleVerifyPasscode(w, r, username)
	case "now-playing":
		h.HandleNowPlaying(w, r, username)
	case "queue":
		h.HandleQueue(w, r, username)
	case "search":
		h.HandleSearch(w, r, username)
	case "add":
		h.HandleAddToQueue(w, r, username)
	case "remove":
		h.HandleRemoveFromQueue(w, r, username)
	default:
		http.NotFound(w, r)
	}
}

type indexData struct {
	LoggedIn bool
}

func (h *Handler) HandleIndex(w http.ResponseWriter, r *http.Request) {
	uid, loggedIn := h.cfg.SessionStore.GetUserID(r)
	if loggedIn {
		if _, err := h.cfg.Store.GetUser(uid); err == nil {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		h.cfg.SessionStore.Clear(w, r)
	}
	h.render(w, "index", indexData{LoggedIn: false})
}

// ── Dashboard ─────────────────────────────────────────────────────────────────

type dashboardData struct {
	User      *models.User
	SharePage *models.SharePage
	ShareURL  string
	QRCodeB64 string
	Playlists []struct{ ID, Name string }
}

func (h *Handler) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.cfg.SessionStore.GetUserID(r)
	user, err := h.cfg.Store.GetUser(uid)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	page, _ := h.cfg.Store.GetSharePage(uid)
	shareURL := fmt.Sprintf("%s/%s?passcode=%s", h.cfg.BaseURL, uid, page.Passcode)

	// Generate QR code
	qrPNG, _ := qrcode.Encode(shareURL, qrcode.Medium, 256)
	qrB64 := base64.StdEncoding.EncodeToString(qrPNG)

	// Fetch playlists for filter setup
	token := &oauth2.Token{
		AccessToken:  user.AccessToken,
		RefreshToken: user.RefreshToken,
		Expiry:       user.ExpiresAt,
	}
	client := spotifyclient.New(token, h.cfg.OAuth)
	playlists, _ := client.GetUserPlaylists()

	data := dashboardData{
		User:      user,
		SharePage: page,
		ShareURL:  fmt.Sprintf("%s/%s", h.cfg.BaseURL, uid),
		QRCodeB64: qrB64,
	}
	if playlists != nil {
		for _, p := range playlists.Items {
			data.Playlists = append(data.Playlists, struct{ ID, Name string }{p.ID, p.Name})
		}
	}

	h.render(w, "dashboard", data)
}

func (h *Handler) HandleTogglePasscode(w http.ResponseWriter, r *http.Request) {
	// Returns partial: the passcode display
	uid, _ := h.cfg.SessionStore.GetUserID(r)
	page, _ := h.cfg.Store.GetSharePage(uid)
	show := r.URL.Query().Get("show") == "1"

	tmplStr := `<span id="passcode-value">`
	if show {
		tmplStr += page.Passcode + `</span><button class="btn-ghost" hx-get="/dashboard/passcode/toggle?show=0" hx-target="#passcode-box" hx-swap="outerHTML">Hide</button>`
	} else {
		tmplStr += `••••••</span><button class="btn-ghost" hx-get="/dashboard/passcode/toggle?show=1" hx-target="#passcode-box" hx-swap="outerHTML">Show</button>`
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div id="passcode-box" class="passcode-box">`+tmplStr+`</div>`)
}

func (h *Handler) HandleRegeneratePasscode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uid, _ := h.cfg.SessionStore.GetUserID(r)
	page, _ := h.cfg.Store.GetSharePage(uid)
	newCode, _ := store.GeneratePasscode()
	page.Passcode = newCode
	page.UpdatedAt = time.Now()
	h.cfg.Store.SaveSharePage(page)

	user, _ := h.cfg.Store.GetUser(uid)
	shareURL := fmt.Sprintf("%s/%s?passcode=%s", h.cfg.BaseURL, uid, newCode)
	qrPNG, _ := qrcode.Encode(shareURL, qrcode.Medium, 256)
	qrB64 := base64.StdEncoding.EncodeToString(qrPNG)

	// Return updated dashboard section
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
<div id="share-info" hx-swap-oob="true">
  <div class="passcode-box" id="passcode-box">
    <span id="passcode-value">••••••</span>
    <button class="btn-ghost" hx-get="/dashboard/passcode/toggle?show=1" hx-target="#passcode-box" hx-swap="outerHTML">Show</button>
  </div>
  <p class="share-url">%s/%s</p>
  <img src="data:image/png;base64,%s" alt="QR Code" class="qr-code" />
</div>
<div id="toast" class="toast">✓ Passcode regenerated</div>
`, h.cfg.BaseURL, user.SpotifyID, qrB64)
}

func (h *Handler) HandleQRCode(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.cfg.SessionStore.GetUserID(r)
	page, _ := h.cfg.Store.GetSharePage(uid)
	shareURL := fmt.Sprintf("%s/%s?passcode=%s", h.cfg.BaseURL, uid, page.Passcode)
	qrPNG, _ := qrcode.Encode(shareURL, qrcode.Medium, 512)
	w.Header().Set("Content-Type", "image/png")
	w.Write(qrPNG)
}

func (h *Handler) HandleUpdateLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uid, _ := h.cfg.SessionStore.GetUserID(r)
	page, _ := h.cfg.Store.GetSharePage(uid)

	r.ParseForm()
	page.Limits.Enabled = r.FormValue("enabled") == "on"
	page.Limits.SongsPerWindow, _ = strconv.Atoi(r.FormValue("songs_per_window"))
	page.Limits.WindowMinutes, _ = strconv.Atoi(r.FormValue("window_minutes"))
	page.Limits.ExplicitAllowed = r.FormValue("explicit_allowed") == "on"
	page.Limits.MaxDurationSec, _ = strconv.Atoi(r.FormValue("max_duration_sec"))
	if g := r.FormValue("genres"); g != "" {
		page.Limits.AllowedGenres = strings.Split(g, ",")
		for i := range page.Limits.AllowedGenres {
			page.Limits.AllowedGenres[i] = strings.TrimSpace(page.Limits.AllowedGenres[i])
		}
	}
	if d := r.FormValue("decades"); d != "" {
		page.Limits.AllowedDecades = strings.Split(d, ",")
		for i := range page.Limits.AllowedDecades {
			page.Limits.AllowedDecades[i] = strings.TrimSpace(page.Limits.AllowedDecades[i])
		}
	}
	if p := r.Form["playlists"]; len(p) > 0 {
		page.Limits.AllowedPlaylists = p
	}
	page.UpdatedAt = time.Now()
	h.cfg.Store.SaveSharePage(page)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div id="toast" class="toast">✓ Limits saved</div>`)
}

// ── Share Page ────────────────────────────────────────────────────────────────

type sharePageData struct {
	Owner       *models.User
	IsVerified  bool
	PasscodeErr string
	GuestSessID string
	BaseURL     string
}

func (h *Handler) HandleSharePage(w http.ResponseWriter, r *http.Request, username string) {
	owner, err := h.cfg.Store.GetUser(username)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	page, _ := h.cfg.Store.GetSharePage(username)

	// Check guest session cookie
	guestSess, _ := auth.GetGuestSession(h.cfg.SessionStore, r, username)
	verifiedPasscode, _ := guestSess.Values["verified_passcode"].(string)
	guestID, _ := guestSess.Values["guest_id"].(string)

	isVerified := verifiedPasscode == page.Passcode

	data := sharePageData{
		Owner:       owner,
		IsVerified:  isVerified,
		GuestSessID: guestID,
		BaseURL:     h.cfg.BaseURL,
	}

	// Auto-verify from URL param if passcode matches
	if !isVerified {
		if pc := r.URL.Query().Get("passcode"); pc == page.Passcode {
			isVerified = true
			data.IsVerified = true
			// Save to cookie
			if guestID == "" {
				guestID, _ = store.GenerateSessionID()
			}
			guestSess.Values["verified_passcode"] = pc
			guestSess.Values["guest_id"] = guestID
			guestSess.Options.MaxAge = 86400 * 365
			guestSess.Save(r, w)
			data.GuestSessID = guestID
		}
	}

	h.render(w, "share", data)
}

func (h *Handler) HandleVerifyPasscode(w http.ResponseWriter, r *http.Request, username string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	owner, err := h.cfg.Store.GetUser(username)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	page, _ := h.cfg.Store.GetSharePage(username)
	r.ParseForm()
	entered := r.FormValue("passcode")

	if entered != page.Passcode {
		h.render(w, "passcode_prompt", sharePageData{
			Owner:       owner,
			PasscodeErr: "Incorrect passcode. Please try again.",
		})
		return
	}

	// Save verified state in guest cookie
	guestSess, _ := auth.GetGuestSession(h.cfg.SessionStore, r, username)
	guestID, _ := guestSess.Values["guest_id"].(string)
	if guestID == "" {
		guestID, _ = store.GenerateSessionID()
	}
	guestSess.Values["verified_passcode"] = entered
	guestSess.Values["guest_id"] = guestID
	guestSess.Options.MaxAge = 86400 * 365
	guestSess.Save(r, w)

	// Create guest session in store
	h.cfg.Store.SaveGuestSession(&models.GuestSession{
		SessionID: guestID,
		OwnerID:   username,
	})

	// Redirect to share page
	http.Redirect(w, r, "/"+username, http.StatusSeeOther)
}

// ── Share Page API endpoints ──────────────────────────────────────────────────

func (h *Handler) requireGuestOrOwner(w http.ResponseWriter, r *http.Request, username string) (guestID string, isOwner bool, ok bool) {
	// Check if owner
	if uid, loggedIn := h.cfg.SessionStore.GetUserID(r); loggedIn && uid == username {
		return uid, true, true
	}

	// Check guest cookie
	page, err := h.cfg.Store.GetSharePage(username)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return "", false, false
	}
	guestSess, _ := auth.GetGuestSession(h.cfg.SessionStore, r, username)
	verifiedPasscode, _ := guestSess.Values["verified_passcode"].(string)
	gid, _ := guestSess.Values["guest_id"].(string)

	if verifiedPasscode != page.Passcode || gid == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false, false
	}
	return gid, false, true
}

func (h *Handler) getOwnerClient(username string) (*spotifyclient.Client, error) {
	user, err := h.cfg.Store.GetUser(username)
	if err != nil {
		return nil, err
	}
	token := &oauth2.Token{
		AccessToken:  user.AccessToken,
		RefreshToken: user.RefreshToken,
		Expiry:       user.ExpiresAt,
	}
	return spotifyclient.New(token, h.cfg.OAuth), nil
}

func (h *Handler) HandleNowPlaying(w http.ResponseWriter, r *http.Request, username string) {
	if _, _, ok := h.requireGuestOrOwner(w, r, username); !ok {
		return
	}

	client, err := h.getOwnerClient(username)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	np, err := client.GetCurrentlyPlaying()
	if err != nil || np == nil || np.Item == nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="now-playing-empty"><span class="np-icon">♪</span><p>Nothing playing right now</p></div>`)
		return
	}

	track := trackFromSpotify(np.Item)
	w.Header().Set("Content-Type", "text/html")
	renderNowPlaying(w, track, np.IsPlaying, np.ProgressMs)
}

func (h *Handler) HandleQueue(w http.ResponseWriter, r *http.Request, username string) {
	if _, _, ok := h.requireGuestOrOwner(w, r, username); !ok {
		return
	}

	client, err := h.getOwnerClient(username)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	q, err := client.GetQueue()
	if err != nil || q == nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="queue-empty"><p>Queue is empty</p></div>`)
		return
	}

	guestSess, _ := auth.GetGuestSession(h.cfg.SessionStore, r, username)
	guestID, _ := guestSess.Values["guest_id"].(string)

	guestSession, _ := h.cfg.Store.GetGuestSessionsByOwner(username, guestID)

	w.Header().Set("Content-Type", "text/html")
	renderQueue(w, q.Queue, guestSession, username)
}

func (h *Handler) HandleSearch(w http.ResponseWriter, r *http.Request, username string) {
	guestID, _, ok := h.requireGuestOrOwner(w, r, username)
	if !ok {
		return
	}

	query := r.URL.Query().Get("q")
	if strings.TrimSpace(query) == "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "")
		return
	}

	page, _ := h.cfg.Store.GetSharePage(username)
	client, err := h.getOwnerClient(username)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	// Build filtered query
	fq := spotifyclient.BuildFilteredQuery(query, page.Limits.AllowedGenres, page.Limits.AllowedDecades)
	results, err := client.SearchTracks(fq, 10)
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<p class="error">Search failed. Try again.</p>`)
		return
	}

	// Filter results
	var tracks []models.Track
	for _, item := range results.Tracks.Items {
		track := trackFromSpotify(&item)

		// Explicit filter
		if !page.Limits.ExplicitAllowed && item.Explicit {
			continue
		}
		// Duration filter
		if page.Limits.MaxDurationSec > 0 && item.DurationMs/1000 > page.Limits.MaxDurationSec {
			continue
		}
		// Decade filter
		if len(page.Limits.AllowedDecades) > 0 {
			year := spotifyclient.TrackYear(item.Album.ReleaseDate)
			decade := spotifyclient.TrackDecade(year)
			allowed := false
			for _, d := range page.Limits.AllowedDecades {
				if d == decade {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		tracks = append(tracks, track)
	}

	w.Header().Set("Content-Type", "text/html")
	renderSearchResults(w, tracks, username, guestID)
}

func (h *Handler) HandleAddToQueue(w http.ResponseWriter, r *http.Request, username string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	guestID, _, ok := h.requireGuestOrOwner(w, r, username)
	if !ok {
		return
	}

	r.ParseForm()
	trackURI := r.FormValue("track_uri")
	trackID := r.FormValue("track_id")
	if trackURI == "" {
		http.Error(w, "missing track_uri", http.StatusBadRequest)
		return
	}

	page, _ := h.cfg.Store.GetSharePage(username)

	// Rate limit check
	if page.Limits.Enabled && page.Limits.SongsPerWindow > 0 && page.Limits.WindowMinutes > 0 {
		since := time.Now().Add(-time.Duration(page.Limits.WindowMinutes) * time.Minute)
		count, _ := h.cfg.Store.CountRecentAdditions(username, guestID, since)
		if count >= page.Limits.SongsPerWindow {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(w, `<div class="error-toast">⚠ Limit reached: max %d song(s) per %d min</div>`, page.Limits.SongsPerWindow, page.Limits.WindowMinutes)
			return
		}
	}

	client, err := h.getOwnerClient(username)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	if err := client.AddToQueue(trackURI); err != nil {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `<div class="error-toast">⚠ Could not add to queue. Is Spotify active?</div>`)
		return
	}

	// Record in guest session
	gs, _ := h.cfg.Store.GetGuestSessionsByOwner(username, guestID)
	if gs == nil {
		gs = &models.GuestSession{SessionID: guestID, OwnerID: username}
	}
	gs.AddedTracks = append(gs.AddedTracks, trackURI)
	gs.AddedAt = append(gs.AddedAt, time.Now())
	h.cfg.Store.SaveGuestSession(gs)

	// Return success with remove button
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
<div class="add-result success" id="track-action-%s">
  <span>✓ Added to queue</span>
  <button class="btn-remove" 
    hx-post="/%s/remove" 
    hx-vals='{"track_uri":"%s","guest_id":"%s"}'
    hx-target="#track-action-%s"
    hx-swap="outerHTML">Remove</button>
</div>`, trackID, username, trackURI, guestID, trackID)
}

func (h *Handler) HandleRemoveFromQueue(w http.ResponseWriter, r *http.Request, username string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	guestID, _, ok := h.requireGuestOrOwner(w, r, username)
	if !ok {
		return
	}

	r.ParseForm()
	trackURI := r.FormValue("track_uri")

	// Remove from guest session record
	gs, err := h.cfg.Store.GetGuestSessionsByOwner(username, guestID)
	if err != nil || gs == nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="error-toast">⚠ Could not find your session</div>`)
		return
	}

	// Check if this guest actually added this track
	found := false
	newTracks := []string{}
	newTimes := []time.Time{}
	for i, t := range gs.AddedTracks {
		if t == trackURI && !found {
			found = true
			continue
		}
		newTracks = append(newTracks, t)
		newTimes = append(newTimes, gs.AddedAt[i])
	}

	if !found {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="error-toast">⚠ You can only remove songs you added</div>`)
		return
	}

	gs.AddedTracks = newTracks
	gs.AddedAt = newTimes
	h.cfg.Store.SaveGuestSession(gs)

	// Note: Spotify doesn't have a remove-from-queue API, we just track it locally
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div class="add-result removed"><span>Removed from queue</span></div>`)
}

// ── Template helpers ──────────────────────────────────────────────────────────

func trackFromSpotify(t *spotifyclient.TrackObject) models.Track {
	artists := make([]string, len(t.Artists))
	for i, a := range t.Artists {
		artists[i] = a.Name
	}
	imgURL := spotifyclient.BestImage(t.Album.Images)
	year := spotifyclient.TrackYear(t.Album.ReleaseDate)
	return models.Track{
		URI:        t.URI,
		ID:         t.ID,
		Name:       t.Name,
		Artists:    artists,
		Album:      t.Album.Name,
		AlbumImage: imgURL,
		DurationMs: t.DurationMs,
		Explicit:   t.Explicit,
		PreviewURL: t.PreviewURL,
		Year:       year,
	}
}

func durationStr(ms int) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func renderNowPlaying(w http.ResponseWriter, track models.Track, isPlaying bool, progressMs int) {
	playIcon := "▶"
	if isPlaying {
		playIcon = "▌▌"
	}
	artists := strings.Join(track.Artists, ", ")
	progressPct := 0
	if track.DurationMs > 0 {
		progressPct = progressMs * 100 / track.DurationMs
	}
	fmt.Fprintf(w, `
<div class="now-playing-card">
  <img src="%s" alt="Album art" class="album-art" />
  <div class="np-info">
    <div class="np-status">%s NOW PLAYING</div>
    <div class="np-title">%s</div>
    <div class="np-artist">%s</div>
    <div class="np-album">%s · %s</div>
    <div class="progress-bar">
      <div class="progress-fill" style="width:%d%%"></div>
    </div>
    <div class="np-time">%s / %s</div>
  </div>
</div>`, track.AlbumImage, playIcon, template.HTMLEscapeString(track.Name),
		template.HTMLEscapeString(artists), template.HTMLEscapeString(track.Album), track.Year,
		progressPct, durationStr(progressMs), durationStr(track.DurationMs))
}

func renderQueue(w http.ResponseWriter, queue []spotifyclient.TrackObject, guestSession *models.GuestSession, username string) {
	if len(queue) == 0 {
		fmt.Fprint(w, `<div class="queue-empty"><p>Queue is empty</p></div>`)
		return
	}

	addedByGuest := map[string]bool{}
	if guestSession != nil {
		for _, uri := range guestSession.AddedTracks {
			addedByGuest[uri] = true
		}
	}

	fmt.Fprint(w, `<ul class="queue-list">`)
	for i, item := range queue {
		if i >= 20 {
			break
		}
		track := trackFromSpotify(&item)
		artists := strings.Join(track.Artists, ", ")
		canRemove := addedByGuest[track.URI]
		removeBtn := ""
		if canRemove {
			removeBtn = fmt.Sprintf(`
<button class="btn-remove-queue" 
  hx-post="/%s/remove"
  hx-vals='{"track_uri":"%s"}'
  hx-target="closest li"
  hx-swap="outerHTML">✕</button>`, username, template.HTMLEscapeString(track.URI))
		}
		fmt.Fprintf(w, `
<li class="queue-item">
  <span class="queue-num">%d</span>
  <img src="%s" alt="" class="queue-thumb" />
  <div class="queue-info">
    <div class="queue-name">%s</div>
    <div class="queue-artist">%s</div>
  </div>
  <span class="queue-dur">%s</span>
  %s
</li>`, i+1, track.AlbumImage, template.HTMLEscapeString(track.Name),
			template.HTMLEscapeString(artists), durationStr(track.DurationMs), removeBtn)
	}
	fmt.Fprint(w, `</ul>`)
}

func renderSearchResults(w http.ResponseWriter, tracks []models.Track, username, guestID string) {
	if len(tracks) == 0 {
		fmt.Fprint(w, `<div class="no-results">No tracks found matching your filters.</div>`)
		return
	}
	fmt.Fprint(w, `<ul class="search-results">`)
	for _, track := range tracks {
		artists := strings.Join(track.Artists, ", ")
		explicitBadge := ""
		if track.Explicit {
			explicitBadge = `<span class="explicit-badge">E</span>`
		}
		fmt.Fprintf(w, `
<li class="search-item" id="search-%s">
  <img src="%s" alt="" class="search-thumb" />
  <div class="search-info">
    <div class="search-name">%s %s</div>
    <div class="search-artist">%s · %s · %s</div>
  </div>
  <div class="search-actions" id="track-action-%s">
    <button class="btn-add"
      hx-post="/%s/add"
      hx-vals='{"track_uri":"%s","track_id":"%s"}'
      hx-target="#track-action-%s"
      hx-swap="outerHTML"
      hx-indicator="#track-action-%s .spinner">
      <span class="add-icon">+</span> Add
    </button>
  </div>
</li>`, track.ID, track.AlbumImage,
			template.HTMLEscapeString(track.Name), explicitBadge,
			template.HTMLEscapeString(artists), template.HTMLEscapeString(track.Album), track.Year,
			track.ID,
			username, template.HTMLEscapeString(track.URI), track.ID, track.ID, track.ID)
	}
	fmt.Fprint(w, `</ul>`)
}

// JSON helper
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
