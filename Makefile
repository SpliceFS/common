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

# Nothing that must stay private may be tracked: no compiled program (it carries the folder
# names of the machine it was built on), no file over 1 MiB, no name of a private machine,
# account or place. The ignore rules help; this check is what stops a mistake.
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
lint: lint-shell
	golangci-lint run

# The shell part of lint by itself, for the CI job that has no Go tools.
.PHONY: lint-shell
lint-shell:
	shellcheck scripts/*.sh

clean:
	rm -rf bin coverage.out coverage.html
