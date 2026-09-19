.PHONY: test build dev copy-web

test:
	cd backend && go test ./...
	cd frontend && pnpm lint && pnpm build

copy-web:
	cd frontend && pnpm build
	rm -rf backend/web/dist
	cp -R frontend/dist backend/web/dist

build: copy-web
	cd backend && go build -o ../bin/compose-manager ./cmd/server

dev:
	CM_DEMO_MODE=true go run ./backend/cmd/server

