.PHONY: build web run dev test tidy clean

# Build the SPA into the embedded directory, then compile the single binary.
build: web
	go build -o loupe ./cmd/loupe

# Run the Go test suite plus the frontend tests (when a runner is configured).
test:
	go test ./...
	cd web && npm test --silent --if-present

web:
	cd web && npm install && npm run build

run: build
	./loupe

# Live-reload development: backend on a fixed port + Vite dev server (proxies to it).
# Run these in two terminals.
dev:
	@echo "terminal 1:  LOUPE_ADDR=127.0.0.1:7878 go run ./cmd/loupe"
	@echo "terminal 2:  cd web && npm run dev   # open the URL it prints"

tidy:
	go mod tidy

clean:
	rm -f loupe
	rm -rf web/node_modules internal/server/web_dist/assets
