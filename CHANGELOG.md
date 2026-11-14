# Changelog

Every merge into `main` is a release with its own version. From 0.3.0 on this file has one
heading per version, newest first, written by the release bot from the pull request. The
changes before that are in one list, below the newer versions.

## [0.3.2] - 2026-11-14

- The protocol names the server and client version headers and the digests a block request carries.

## [0.3.1] - 2026-11-14

- Internal: a release of the shared code now opens the pull request that moves the server to it, and
  the server's release does the same for the client.

## [0.3.0] - 2026-11-14

- The protocol names a second digest of a file, the tail digest: the SHA-256 of its last 64 KiB,
  sent in the header `X-SpliceFS-Tail-Digest`.

## [0.1.1 to 0.2.3]

### Added

- `units`: fuzz tests for the size and duration parsing, and `make fuzz` to run them.
- Trunk development: one branch, `main`, and one version per change.

## [0.1.0] - 2026-11-11

### Added

- `wire`: the JSON bodies and header names of the HTTP API, version 1.
- `units`: the size and duration types used in the configuration files.
- Code quality checks at their strictest: every linter is on, and each one that is off says
  why in `.golangci.yml`. `units` now declares its errors (`ErrNoNumber`, `ErrNoUnit`,
  `ErrUnknownUnit`, `ErrNotWhole`, `ErrTooLarge`), so callers can test for them.
- `make lint` also runs ShellCheck, with every optional check on, and `make check` fails when a
  compiled program or a file over 1 MiB is tracked.
- `make check` fails when a tracked file names a private machine, account or place, and the
  ignore rules cover programs built in place, editor leftovers and disk images.
