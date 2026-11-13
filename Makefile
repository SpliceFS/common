# Image for running the Linux checks from another system.
LINUX_IMAGE ?= golang:1.27
LINUX_RUN := docker run --rm -v "$(CURDIR)":/src:ro -v splicefs-gomod:/go/pkg/mod $(LINUX_IMAGE)

.PHONY: all build check check-linux fmt fmt-check vet test cover fuzz lint clean

all: check build

# A library has no program to produce; this proves every package compiles.
build:
	go build ./...

# Everything CI runs except the linter.
check: fmt-check vet cover tracked-files

# A compiled program or a very large file must never be committed: a program built on a
# developer's machine carries the folder names of that machine.
.PHONY: tracked-files
tracked-files:
	scripts/tracked-files.sh

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "not gofmt formatted:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

test:
	go test -race ./...

# Tests with the race detector; fails below 100% statement coverage.
cover:
	scripts/coverage.sh coverage.out

# Go runs one fuzz target per invocation, so the targets are found with go test -list and
# run one by one; a new Fuzz function is picked up without editing this file.
FUZZTIME ?= 10s
fuzz:
	@set -e; for pkg in $$(go list ./...); do \
		for target in $$(go test -list '^Fuzz' $$pkg | grep '^Fuzz'); do \
			echo "fuzz $$pkg $$target ($(FUZZTIME))"; \
			go test -run '^$$' -fuzz "^$$target\$$" -fuzztime $(FUZZTIME) $$pkg; \
		done; \
	done

# The same checks on Linux, from any system that has Docker.
# The source is copied inside the container so nothing is written to the working tree.
check-linux:
	$(LINUX_RUN) sh -c 'cp -r /src /tmp/w && cd /tmp/w && make check'

# ShellCheck runs with every optional check on (see .shellcheckrc).
lint:
	golangci-lint run
	shellcheck scripts/*.sh

clean:
	rm -rf bin coverage.out coverage.html
