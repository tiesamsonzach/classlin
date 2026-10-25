.PHONY: test server

test:
	cd server && go test ./...
	cd desktop/core && cargo test

server:
	cd server && PORT=8787 go run ./cmd/server
