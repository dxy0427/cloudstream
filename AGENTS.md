# AGENTS.md - CloudStream

Go 1.26/Gin/GORM-SQLite backend plus a Vue 3/Vite 6 JavaScript frontend. This is one Go module plus `frontend/`; there is no workspace or task runner.

## Commands

```bash
# Backend; go-sqlite3 requires CGO and a working C toolchain.
CGO_ENABLED=1 go test ./...
CGO_ENABLED=1 go test ./internal/api/handlers -run '^TestUpdateAccountRejectsMalformedID$'
CGO_ENABLED=1 go build -o cloudstream ./cmd/cloudstream
./cloudstream

# Frontend; Node 20 matches the Docker build and package-lock.json is authoritative.
cd frontend
npm ci
npm run dev
npm test
npm test -- src/composables/useSecretVisibility.test.js -t 'tracks each field locally and independently'
npm run build

# This multi-stage build compiles both frontend and backend; no host prebuild is needed.
docker build -t cloudstream .
```

## Runtime Wiring

- Run the binary from the repository root. Runtime paths are relative: `data/cloudstream.db`, `data/.jwt_secret`, `data/cloudstream.log`, and static files under `public/`. The `data/` directory is created automatically.
- `cmd/cloudstream/main.go` starts two fixed listeners: API/control/static UI on `12398`, and the media-server proxy on `8091`. Vite proxies `/api` to `localhost:12398`; `8091` is also enforced by `internal/mediaserver`.
- `npm run build` writes `frontend/dist/`. Only the Dockerfile copies that output to the backend's `public/`; for local UI development use Vite alongside the backend.
- Startup initializes the JWT secret, runs GORM `AutoMigrate`, loads scheduled tasks, then reloads media-server proxies. API routes are rooted at `/api/v1` in `internal/api/router.go`.

## Auth And API Contracts

- First run creates user `admin`. Set `CLOUDSTREAM_ADMIN_PASSWORD`, or capture the generated password printed once to stderr. Browser login sends SHA-256(password); the database stores bcrypt(SHA-256(password)).
- Reset the sole existing administrator with `./cloudstream admin password [NEW_PASSWORD]`. Omitting the password generates and prints one; the command requires the existing database, invalidates sessions, and does not start either server.
- Sessions use the HTTP-only `cloudstream_token` cookie backed by `data/.jwt_secret`; frontend code must not expect to read the token.
- Account, media-server, and notification updates use optimistic concurrency via `Version`. List endpoints omit secrets; authenticated detail endpoints return them with no-store headers. Treat an omitted secret field differently from an explicit empty value.

## Frontend Contracts

- `frontend/src/api/index.js` unwraps Axios responses to the JSON body, so callers receive `{ code, data, ... }`, not an Axios response object.
- Vue APIs, selected Naive UI hooks, and Naive UI components are auto-imported by `vite.config.js`; icon components still need explicit imports. Alias `@` resolves to `frontend/src`.
- Vue Router uses history mode; the Go router provides the `index.html` fallback for non-API GET/HEAD routes.

## Test And CI Quirks

- `internal/integration/TestLiveConnections` only contacts real services when the corresponding `CLOUDSTREAM_TEST_WEBDAV_*`, `CLOUDSTREAM_TEST_OPENLIST_*`, or `CLOUDSTREAM_TEST_EMBY_*` variables are complete; otherwise those subtests skip.
- Auth signature tests create the ignored file `internal/auth/data/.jwt_secret` because the secret path is relative to the package test working directory.
- There is no repository lint, typecheck, codegen, or pre-commit command. GitHub Actions only provides a manual Docker build/push workflow and does not run tests.
