# AGENTS.md

Guidance for AI coding agents working on this repo (Timeful / Schej.it fork): a group availability / scheduling app.

## Repo layout

Monorepo:

- `frontend/` — Vue 2 + Vuetify + Tailwind SPA (Vue CLI). Built into `frontend/dist`.
- `server/` — Go (Gin) HTTP API backed by MongoDB. Also serves the built frontend as static files at the root.
- `compose.yaml` — Docker Compose: `mongo` + `frontend` (build-only) + `server`. See `DEPLOYMENT.md`.
- `PLUGIN_API_README.md` — `window.postMessage` API used by browser plugins to read/write availability on the frontend.

Internal identifiers (Go module `schej.it/server`, Mongo DB `schej-it`) still use the old name — leave them unless rebranding is the explicit task.

## Common commands

### Frontend (`cd frontend`)
- `npm run serve` — dev server on `:8080`.
- `npm run build` — production build into `frontend/dist`.
- `npm run test:unit` — Vitest (`vitest.config.mjs`, alias `@` → `src/`).
- Single test: `npx vitest run src/utils/date_utils.test.js` (or `-t "name"`).

### Backend (`cd server`)
- `air` — live-reload dev (install via `go install github.com/cosmtrek/air@latest`). Listens on `:3002` (`:3003` if `NODE_ENV=staging`).
- `go run main.go` — run without live reload. `-release` forces `GIN_MODE=release`.
- `go test $(go list ./... | grep -v '/scripts/' | grep -v '/db$' | grep -v '/services/listmonk$')` — run backend tests that don’t require Mongo/Listmonk. Notes:
  - `server/scripts/` are dated migrations and are not kept compiling.
  - `./db` tests expect Mongo initialized.
  - `./services/listmonk` tests expect Listmonk env configured.
- `swag init` (in `server/`) — regenerate Swagger docs in `server/docs/` after editing route comments. UI at `http://localhost:3002/swagger/index.html`.

### Local dev with Mongo from compose

`compose.yaml` does **not** publish Mongo's port to the host (the in-container `server` reaches it via the compose network). If you run the Go server on the host (`air` / `go run` / a binary outside Docker), start Mongo with the port published, e.g.:

```
docker run -d --name timeful-mongo-1 \
  -p 127.0.0.1:27017:27017 \
  -v timeful_mongo_data:/data/db \
  --restart unless-stopped mongo:7
```

## Architecture notes

### Backend (Gin + MongoDB)
`server/main.go` wires CORS, cookie sessions, Mongo init (`db.Init`), then mounts API groups under `/api` via `routes.Init*`. After API routes, it serves `frontend/dist` and falls back to `NoRoute` for SPA routes.

- `routes/` — handlers grouped by domain (`auth.go`, `user.go`, `users.go`, `events.go`, `folders.go`, `analytics.go`, `stripe.go`). Route comments use Swag annotations; `swag init` regenerates `docs/`.
- `models/` — Mongo document structs.
- `db/` — Mongo accessors per model + `init.go`. Treat this as the only layer that talks to Mongo.
- `services/auth/` — OAuth + calendar-linking helpers.
- `scripts/` — dated one-off Mongo migrations. Run manually; don't import from runtime code.
- `routes/hackclub.go` — Hack Club OAuth integration (see Hack Club OAuth section below).

### Frontend (Vue 2 SPA)
- `src/router/index.js` — routes (`Landing`, `Home`, `Event`, `Group`, `Friends`, `Settings`, `SignIn`/`SignUp`/`Auth`, `StripeRedirect`, etc.).
- `src/store/index.js` — single Vuex store.
- `src/components/` — organized by feature folder (`event/`, `groups/`, `home/`, `landing/`, `settings/`, etc.) plus shared components.
- `src/utils/` — `fetch_utils.js`, `plugin_utils.js`, `sign_in_utils.js`, `services/`.

### Plugin (browser extension) API
Frontend exposes `get-slots` / `set-slots` over `window.postMessage` with a `FILL_CALENDAR_EVENT` type and `requestId`. Implementation in `src/utils/plugin_utils.js`; spec in `PLUGIN_API_README.md`. Don’t change message shapes without updating that doc.

## Conventions

- Go module path is `schej.it/server`. Don’t rename.
- The server panics on startup if `SESSION_SECRET` is missing or shorter than 32 chars (`validateSessionSecret` in `main.go`).
- **No paywalls or ads.** Don’t add event limits, ad network scripts, or upgrade gating UI. (As part of repo cleanup, unused `pricing/` dialogs and legacy ad images were removed.)

### Hack Club OAuth Integration
Hack Club (`auth.hackclub.com`) provides an alternative sign-in method for Hack Club members. The integration is fully implemented end-to-end:

**Frontend flow** (`frontend/src/views/SignIn.vue`):
1. User clicks "Sign in with Hack Club"
2. Frontend redirects to `https://auth.hackclub.com/oauth/authorize` with:
   - `client_id`: from `HACKCLUB_CLIENT_ID` env var
   - `redirect_uri`: `http://localhost:8080/auth` (configurable per environment)
   - `scope`: `email name slack_id` (Hack Club requires slack_id in the scope)
   - `state`: JSON object with `type: "hackclub"`
3. Hack Club redirects back to `/auth?code=...&state=...`
4. `Auth.vue` component (`frontend/src/views/Auth.vue`):
   - Extracts code and state from query params
   - POSTs to `/api/auth/hackclub` with code and timezone offset
   - Sets Vuex auth user and redirects to home

**Backend flow** (`server/routes/hackclub.go`):
1. Receives authorization code from frontend
2. Exchanges code for access token at `https://auth.hackclub.com/oauth/token`:
   - Sends `client_id`, `client_secret`, `code`, `redirect_uri`
   - Expects JSON response with `access_token` (error response: `{"error": "invalid_grant", ...}`)
   - **Important:** `redirect_uri` must exactly match what was sent to authorize endpoint (see below)
3. Fetches user identity from `https://auth.hackclub.com/api/v1/me`:
   - Sends Bearer token in Authorization header
   - Extracts `slack_id`, `email`, `first_name`, `last_name`
   - Validates: slack_id must be present (returns 400 if missing)
4. Enriches displayName via `https://cachet.dunkirk.sh/users/{slack_id}`:
   - Optional call that may fail gracefully
   - Gets `displayName` and `imageUrl` for the user profile
   - Falls back to Hack Club's `first_name last_name` if lookup fails
5. Upserts user in MongoDB:
   - Looks up by slack_id first, then by email
   - Creates new user if not found
   - Updates existing user (preserves custom name if set)
6. Sets session cookie and returns user object

**Environment variables** (in `server/.env`):
- `HACKCLUB_CLIENT_ID` — OAuth app ID from Hack Club Dashboard
- `HACKCLUB_CLIENT_SECRET` — OAuth app secret
- `HACKCLUB_REDIRECT_URI` — **Critical for local dev!** Must match frontend's redirect_uri. 
  - Local dev: `http://localhost:8080/auth`
  - Production: `https://timeful.app/auth` (or your domain)
  - If not set, falls back to deriving from incoming request's Origin header (may cause mismatches in dev)

**Common issues & debugging:**
- **400 invalid_grant**: Redirect URI mismatch. Check that `HACKCLUB_REDIRECT_URI` env var matches what frontend sends to Hack Club.
- **400 hackclub-missing-slack-id**: User's Hack Club identity lacks a slack_id. This happens if:
  - User hasn't linked their Slack account in Hack Club
  - Slack ID field is empty in their Hack Club profile
  - Solution: Have user link their Slack account via auth.hackclub.com settings
- **User created but not logged in**: Session not persisting. Verify:
  - CORS has `AllowCredentials: true` (set in `main.go`)
  - Frontend sends credentials in fetch (set to "include" in `fetch_utils.js`)
  - `SESSION_SECRET` is configured and is at least 32 characters

**Testing the flow locally:**
```bash
# Terminal 1: Start backend (in server/)
go run main.go -release

# Terminal 2: Start frontend (in frontend/)
npm run serve

# Browser:
1. Go to http://localhost:8080/sign-in
2. Click "Sign in with Hack Club"
3. Log in with Hack Club credentials
4. Redirected to /auth?code=...&state=...
5. Auth.vue exchanges code for token and creates/updates user
6. Redirected to home page
```

**Recent fixes (May 2026):**
- Fixed redirect URI mismatch by adding `HACKCLUB_REDIRECT_URI` env var fallback. Previously, using only Origin header caused mismatches when frontend and backend are on different ports.
- Added explicit handler for `HACKCLUB` auth type in `Auth.vue` to properly redirect post-login. Previously fell through to default case.
