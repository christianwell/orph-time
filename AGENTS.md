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
