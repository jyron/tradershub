# bottrade dev/prod tooling.
#
# Local dev uses .env.local (file:./bottrade.db) so we never touch Turso.
# Production reads .env. The Go binary loads both — .env.local wins where it
# overlaps.

BINARY        := bottrade
DEV_DB        := ./bottrade.db
DEV_PORT      ?= 3000

.PHONY: help dev run build prod-bin reset test fmt vet tidy clean stop logs

help:
	@echo "Targets:"
	@echo "  dev             run server with .env.local (local SQLite, no Turso)"
	@echo "  build           compile $(BINARY) for the current platform"
	@echo "  prod-bin        compile a stripped/optimized linux/amd64 binary"
	@echo "  reset           wipe local SQLite DB"
	@echo "  test            go test ./..."
	@echo "  fmt             gofmt -w ."
	@echo "  vet             go vet ./..."
	@echo "  tidy            go mod tidy"
	@echo "  stop            kill any locally-running bottrade process"
	@echo "  clean           remove built binaries and dev state files"

dev: stop
	@echo "→ starting dev server on :$(DEV_PORT) against $(DEV_DB)"
	@go run .

run: build
	./$(BINARY)

build:
	go build -o $(BINARY) .

prod-bin:
	@echo "→ building stripped linux/amd64 binary"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o $(BINARY) .

reset: stop
	@echo "→ wiping $(DEV_DB)*"
	@rm -f $(DEV_DB) $(DEV_DB)-wal $(DEV_DB)-shm $(DEV_DB)-journal
	@rm -f .dev_bot.json .test_workflow_state
	@echo "✓ local state cleared. Next 'make dev' starts fresh."

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

stop:
	@# `go run .` execs a compiled binary in $$TMPDIR, so a pattern like
	@# "go run ." doesn't match the running child. Kill by port: it's the
	@# one signal we know is correct.
	@lsof -ti:$(DEV_PORT) 2>/dev/null | xargs -r kill -9 2>/dev/null || true
	@pkill -f "go run \." 2>/dev/null || true
	@true

logs:
	@tail -f /tmp/bottrade-dev.log 2>/dev/null || echo "no dev log at /tmp/bottrade-dev.log"

clean: stop
	@rm -f $(BINARY) .dev_bot.json
	@echo "✓ cleaned"
