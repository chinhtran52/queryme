VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PREFIX  ?= $(HOME)/.local
LDFLAGS := -s -w -X github.com/chinhtran52/queryme/internal/cli.version=$(VERSION)
TEST_DB ?= postgres://postgres:queryme@localhost:55432/postgres

.PHONY: build install uninstall test test-db db-up db-down snapshot clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/queryme ./cmd/queryme

install: build
	install -Dm755 bin/queryme $(PREFIX)/bin/queryme
	@echo "installed $(PREFIX)/bin/queryme ($(VERSION))"

uninstall:
	rm -f $(PREFIX)/bin/queryme

test:
	go vet ./...
	go test ./...

# Runs the Postgres-backed tests against the container started by db-up.
test-db:
	QUERYME_TEST_DATABASE_URL=$(TEST_DB) go test -count=1 ./...

db-up:
	docker run -d --rm --name queryme-pg -e POSTGRES_PASSWORD=queryme -p 55432:5432 postgres:17-alpine

db-down:
	docker stop queryme-pg

# Builds every release target locally into dist/ without publishing.
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin dist
