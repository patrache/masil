.PHONY: test test-short golden golden-pkgs lint build cross

test:
	go test ./...

test-short:
	go test -short ./...

# golden-pkgs prints, one per line, every package whose test binary links
# github.com/charmbracelet/x/exp/golden (only those define the -update flag).
golden-pkgs:
	@for p in $$(go list ./...); do go list -test -deps $$p | grep -qx 'github.com/charmbracelet/x/exp/golden' && echo $$p; done; true

golden:
	@pkgs="$$($(MAKE) -s --no-print-directory golden-pkgs)"; test -n "$$pkgs" || { echo "no golden packages"; exit 1; }; go test $$pkgs -args -update

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"

build:
	CGO_ENABLED=0 go build -o bin/masil ./cmd/masil

cross:
	for os in darwin linux windows; do CGO_ENABLED=0 GOOS=$$os go build ./... || exit 1; done
