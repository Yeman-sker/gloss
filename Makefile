# Every command the project needs. See docs/DEVELOPMENT.md.
RELEASE_FLAGS = -trimpath -tags mygo_noinspector -ldflags "-s -w -X github.com/egoist/mygo.production=1"

.PHONY: build helper run test check app release stats icons
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

# The identity that signs Gloss.app. A certificate's, unlike ad hoc ("-"), is
# the same for every build, so macOS keeps the accessibility permission when an
# update replaces the app. Without the certificate: make app SIGN=-
SIGN ?= Apple Development

VERSION := $(shell sed -n 's/^ *"version": "\(.*\)",/\1/p' mygo.json)

# The secret half of mygo.json's updates.publicKey, kept out of the repository.
UPDATE_KEY ?= $(HOME)/Library/Application Support/mygo/update-keys/gloss/mygo-update.key

# A signed Gloss.app in build/, with the helper in its Resources: needed for
# "登录时启动" and for the permission to stick to one app.
app: helper
	go tool mygo build -sign "$(SIGN)"

# make app, plus the signed update, uploaded with the disk image to the draft
# release of this version. See docs/DEVELOPMENT.md#发布.
release: helper
	@test -z "$$(git status --porcelain)" || { echo "release: 工作区不干净"; exit 1; }
	@test -r "$(UPDATE_KEY)" || { echo "release: 没有更新私钥 $(UPDATE_KEY)"; exit 1; }
	MYGO_UPDATER_PRIVATE_KEY="$$(cat "$(UPDATE_KEY)")" go tool mygo build -sign "$(SIGN)" -upload
	@# install.sh downloads without a login, which a private repository refuses.
	gh release delete-asset v$(VERSION) install.sh --yes

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
