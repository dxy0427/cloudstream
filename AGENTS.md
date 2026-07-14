# AGENTS.md — CloudStream

Go 1.26 backend (Gin + GORM/SQLite) + Vue 3 frontend (Vite 6) in a single repo. No task runner, no monorepo tooling.

## Commands

```bash
# Frontend dev (proxies /api → localhost:12398)
cd frontend && npm install && npm run dev

# Backend build/run (requires CGO + SQLite dev headers)
CGO_ENABLED=1 go build -o cloudstream ./cmd/cloudstream
./cloudstream          # writes ./data/cloudstream.db

# Docker (frontend+vite must build first; output → public/)
docker build -t cloudstream .
```

## Critical Setup

- `./data/` must exist before first run (SQLite lives here). Gitignored.
- Backend listens on **12398** (API) + **8091** (media proxy). Change one, update the frontend proxy target in `frontend/vite.config.js:50`.
- First-run user is `admin`; set `CLOUDSTREAM_ADMIN_PASSWORD` or read the one-time random password printed to stderr. Browser login sends SHA-256(password), then the backend verifies bcrypt(SHA-256(password)).
- To recover a legacy administrator that still uses the old password format, set `CLOUDSTREAM_RESET_ADMIN_PASSWORD` for one startup, then remove it.

## Frontend Notes

- No package-lock.json; `package-lock.json` is gitignored. Use `npm install`, not `npm ci`.
- Naive UI components auto-resolved — no manual imports needed.
- Path alias `@` = `frontend/src/`.
- Build output goes to `frontend/dist/`; Docker stage copies it to `./public/`.

## Exclusions / Gaps

- Focused Go regression tests exist under `internal/**`; there is no frontend test runner. No Makefile or lint/typecheck config. CI only does manual Docker builds to GHCR.
- No pre-commit hooks, no linter, no codegen step.
