# mermaid-render is a library. The only binary is the development CLI under
# tools/ (mermaid file -> PNG), built into dist/ for hands-on checks and E2E.
# It is never released.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
DIST_DIR := dist

.PHONY: build test vet clean

build:
	@mkdir -p $(DIST_DIR)
	go build $(LDFLAGS) -o $(DIST_DIR)/mmdpng ./tools/mmdpng

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf $(DIST_DIR)
