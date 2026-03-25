.PHONY: build build-daemon build-channel dev dev-channel test test-daemon test-channel install clean

# Build
build: build-daemon build-channel

build-daemon:
	cd daemon && CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/

build-channel:
	cd channel && bun install

# Dev
dev:
	cd daemon && go run ./cmd/gobrrr/ daemon start

dev-channel:
	cd channel && bun run index.ts

# Test
test: test-daemon test-channel

test-daemon:
	cd daemon && go test ./...

test-channel:
	@echo "No channel tests yet"

# Install
install: build
	cp daemon/gobrrr ~/.local/bin/gobrrr
	cd channel && bun install
	@echo ""
	@echo "Binary installed to ~/.local/bin/gobrrr"
	@echo "Run 'gobrrr setup' to configure"

clean:
	rm -f daemon/gobrrr
