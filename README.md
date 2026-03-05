# 🎵 Jukebox

A Go + HTMX web app that lets you share your Spotify queue with others via a passcode-protected link. Guests can search and add songs — you control the limits.

## Features

- **Spotify OAuth login** — persistent session (30 days)
- **Share page** at `/{spotify_username}` — passcode-protected
- **QR code** with passcode auto-filled in the URL
- **Passcode management** — show/hide, regenerate any time
- **Live now-playing + queue** — auto-refreshes every 10–15 seconds
- **Song search** — debounced, live results
- **Add to queue** — guests can add; they can remove only what they added
- **Rate limiting** — e.g., 1 song per minute per guest
- **Content filters** — explicit toggle, max duration, allowed genres, decades, playlists

## Tech Stack

| Layer    | Tech                                |
| -------- | ----------------------------------- |
| Backend  | Go 1.22, `net/http` stdlib router   |
| Frontend | HTMX 1.9, vanilla CSS               |
| Auth     | Spotify OAuth2 via gorilla/sessions |
| QR Code  | skip2/go-qrcode                     |
| Storage  | In-memory (swap for Redis/Postgres) |

## Getting Started

### 1. Create a Spotify App

1. Go to [developer.spotify.com/dashboard](https://developer.spotify.com/dashboard)
2. Create a new app
3. Set the Redirect URI to: `http://127.0.0.1:8080/auth/callback`
4. Copy the Client ID and Client Secret

### 2. Configure Environment

```bash
cp .env.example .env
# Edit .env with your Spotify credentials
```

### 3. Run

```bash
# Install dependencies
go mod tidy

# Run (with env vars loaded from .env via Makefile)
make run

# Or with hot-reload (requires air)
make dev
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080)

## Project Structure

```
jukebox/
├── cmd/server/main.go          # Entry point, routing
├── internal/
│   ├── auth/auth.go            # Spotify OAuth + session store
│   ├── handlers/handlers.go    # All HTTP handlers
│   ├── middleware/auth.go      # Auth middleware
│   ├── models/models.go        # Data types
│   ├── spotify/client.go       # Spotify API wrapper
│   └── store/memory.go         # In-memory store + helpers
├── templates/
│   ├── layouts/base.html       # Base HTML layout
│   └── pages/                  # Page templates
│       ├── index.html          # Login page
│       ├── dashboard.html      # Owner dashboard
│       ├── share.html          # Guest share page
│       └── passcode_prompt.html
├── static/
│   ├── css/app.css             # All styles
│   └── js/app.js               # HTMX helpers
├── .env.example
├── .air.toml                   # Hot reload config
└── Makefile
```

## How It Works

### Owner Flow

1. Log in with Spotify
2. Dashboard shows your share URL + passcode (hidden by default)
3. Share the QR code or URL with guests
4. Configure rate limits and content filters
5. Regenerate passcode anytime to lock out current guests

### Guest Flow

1. Open the share link (passcode auto-fills from URL, or enter manually)
2. See the owner's now-playing track and queue
3. Search for songs and add them to the queue
4. Remove songs you added (before they play)

### Passcode Security

- Passcodes are stored in the owner's share page record
- Guest cookies store the verified passcode value
- If the owner regenerates the passcode, old guest cookies become invalid — guests must re-enter the new code

## Production Notes

### Replace In-Memory Store

The current `MemoryStore` loses data on restart. For production, implement the `Store` interface with:

- **Redis** — for sessions + rate limiting
- **PostgreSQL** — for users, share pages, guest sessions

### Token Refresh

The Spotify `oauth2.TokenSource` handles refresh automatically. Make sure to persist the updated token back to your store.

### HTTPS Required

Spotify OAuth requires HTTPS in production. Set `BASE_URL=https://yourdomain.com` and run behind a reverse proxy (nginx, Caddy).

### Environment Variables

| Variable                | Description                                 |
| ----------------------- | ------------------------------------------- |
| `SPOTIFY_CLIENT_ID`     | From Spotify Developer Dashboard            |
| `SPOTIFY_CLIENT_SECRET` | From Spotify Developer Dashboard            |
| `SESSION_SECRET`        | Random 32–64 char string for cookie signing |
| `BASE_URL`              | Full URL (no trailing slash)                |
| `PORT`                  | Port to listen on (default: 8080)           |

## Note on Queue Removal

Spotify's Web API does not support removing tracks from the queue. The remove button tracks which songs a guest added (stored in their session) and records the removal locally — but the song will still play unless the owner skips it. This is a Spotify API limitation.
