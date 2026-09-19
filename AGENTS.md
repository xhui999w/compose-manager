# Compose Manager contributor guide

## Product guardrails

- Compose-first: do not add unrelated Docker/NAS administration features.
- Desktop-first and dense: prefer tables, inline actions, drawers, and compact controls over cards.
- Safety before convenience: destructive operations require explicit confirmation and a server-side recheck.
- Recoverability: back up a Compose file before replacement; never partially overwrite a working file.
- The browser never talks to the Docker socket and never submits shell commands.

## Repository layout

- `backend/`: Go HTTP API, Docker/Compose adapters, SQLite persistence.
- `frontend/`: React, TypeScript, Vite, Ant Design.
- `docs/`: product, architecture, plan, and design reference.
- `data/`: runtime SQLite data (ignored except `.gitkeep`).
- `backups/`: runtime Compose backups (ignored except `.gitkeep`).

## Required checks

Before marking a task complete:

1. Run `go test ./...` in `backend/`.
2. Run `pnpm lint` and `pnpm build` in `frontend/`.
3. Run `go vet ./...` when Go is available.
4. Update `docs/TASKS.md` truthfully.
5. Do not leave placeholder TODOs in a completed phase.

## Security rules

- Resolve and validate Compose paths against configured scan roots before any file access.
- Invoke `docker` with an argument vector through `exec.CommandContext`; never invoke a shell.
- Keep the Compose operation allowlist in the backend.
- Validate project/service/container/image identifiers before passing them to Docker.
- Recompute image references immediately before deletion.
- Write edits to a sibling temporary file, validate, back up, then atomically replace.
- Do not log secrets, `.env` values, registry credentials, or full request bodies containing YAML.

## Style

- Keep handlers thin and business logic in services.
- Depend on small interfaces around Docker, Compose execution, filesystem, and persistence.
- Return structured API errors with stable codes; keep raw subprocess output server-side where it may contain secrets.
- Keep React pages split by feature; `App` is composition and routing only.
- User-facing UI is Chinese-first and accessible by keyboard.

