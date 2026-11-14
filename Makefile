.PHONY: check web build test vet release

# Version stamped into the binary: the nearest tag, or "dev" without git.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Targets for `make release`, as os/arch[/arm version].
PLATFORMS := linux/amd64 linux/arm64 linux/arm/7

# The frontend is built first because the Go binary embeds web/dist.
check: web vet test build

web:
	cd web && { test -d node_modules || npm ci --no-audit --no-fund; } && npm run build

vet:
	go vet ./...

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/sshud ./cmd/sshud

# Cross-compile static binaries into dist/ as sshud_<version>_linux_<arch>.tar.gz
# (binary, LICENSE, README), plus sha256 checksums.txt.
release: web
	rm -rf dist && mkdir -p dist
	@set -e; for p in $(PLATFORMS); do \
		os=$$(echo $$p | cut -d/ -f1); arch=$$(echo $$p | cut -d/ -f2); arm=$$(echo $$p | cut -d/ -f3); \
		name=sshud_$(VERSION)_$${os}_$$arch; [ -n "$$arm" ] && name=$${name}v$$arm; \
		echo "building $$name"; \
		mkdir -p dist/$$name; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch GOARM=$$arm \
			go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$$name/sshud ./cmd/sshud; \
		cp LICENSE README.md dist/$$name/; \
		COPYFILE_DISABLE=1 tar -C dist/$$name -czf dist/$$name.tar.gz sshud LICENSE README.md; \
		rm -rf dist/$$name; \
	done
	cd dist && { command -v sha256sum >/dev/null && sha256sum *.tar.gz || shasum -a 256 *.tar.gz; } > checksums.txt
	cat dist/checksums.txt
