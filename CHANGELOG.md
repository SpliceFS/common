# Changelog

All notable changes to this project are recorded here. Every merge into `main` is a
release with its own version, so there are no version headings: the
[release page](../../releases) of a version names its change, and this file collects the
notable ones.

## Changes

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
