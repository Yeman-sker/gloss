# Every command the project needs. See docs/DEVELOPMENT.md.
RELEASE_FLAGS = -trimpath -tags mygo_noinspector -ldflags "-s -w -X github.com/egoist/mygo.production=1"

.PHONY: build helper run test check app stats icons
# bin/ for make run, resources/darwin/ for make app and mygo dev.
helper:
	cd capture/helper && cargo build --release
	mkdir -p bin resources/darwin
	cp capture/helper/target/release/gloss-capture bin/
	cp capture/helper/target/release/gloss-capture resources/darwin/

build: helper
	go build $(RELEASE_FLAGS) -o bin/gloss .

run: build
	./bin/gloss

# A signed Gloss.app in build/, with the helper in its Resources: needed for
# "登录时启动" and for the permission to stick to one app.
app: helper
	go tool mygo build

# Bakes design/icons into app/icons, and the menu bar's icon into app/tray.png.
# Needs Node and Chrome, the app needs neither: the results are checked in, so
# this runs only when the icons change.
icons:
	node design/icons/scripts/bake.mjs
	node design/icon/export-tray.mjs

# What memory says about routing, from the records on this machine.
stats:
	go run . stats

test:
	go test ./...
	cd capture/helper && cargo test

# Run before saying a change is done.
check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...
	cd capture/helper && cargo build --release
