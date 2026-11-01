# Every command the project needs. See docs/DEVELOPMENT.md.
RELEASE_FLAGS = -trimpath -tags mygo_noinspector -ldflags "-s -w -X github.com/egoist/mygo.production=1"

.PHONY: build helper run test check
helper:
	cd capture/helper && cargo build --release
	mkdir -p bin
	cp capture/helper/target/release/gloss-capture bin/

build: helper
	go build $(RELEASE_FLAGS) -o bin/gloss .

run: build
	./bin/gloss

test:
	go test ./...
	cd capture/helper && cargo test

# Run before saying a change is done.
check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...
	cd capture/helper && cargo build --release
