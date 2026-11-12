# SpliceFS common

[![CI](https://github.com/SpliceFS/common/actions/workflows/ci.yml/badge.svg)](https://github.com/SpliceFS/common/actions/workflows/ci.yml)
[![Security](https://github.com/SpliceFS/common/actions/workflows/security.yml/badge.svg)](https://github.com/SpliceFS/common/actions/workflows/security.yml)
[![Coverage](https://codecov.io/gh/SpliceFS/common/graph/badge.svg)](https://codecov.io/gh/SpliceFS/common)
[![Quality gate](https://sonarcloud.io/api/project_badges/measure?project=SpliceFS_common&metric=alert_status)](https://sonarcloud.io/summary/overall?id=SpliceFS_common)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/SpliceFS/common/badge)](https://scorecard.dev/viewer/?uri=github.com/SpliceFS/common)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/16607/badge)](https://www.bestpractices.dev/projects/16607)
[![Go Reference](https://pkg.go.dev/badge/github.com/SpliceFS/common.svg)](https://pkg.go.dev/github.com/SpliceFS/common)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The Go packages that the SpliceFS [server](https://github.com/SpliceFS/server) and
[client](https://github.com/SpliceFS/client) both use. Each program names the version of this
module it is built against in its `go.mod`.

| Package | What it holds |
|---|---|
| `wire` | the JSON bodies and header names of the HTTP API, version 1 |
| `units` | the size and duration types used in the configuration files |

A package belongs here only if both programs need it, and it is named for what it does.

## Use

```
go get github.com/SpliceFS/common
```

```go
import "github.com/SpliceFS/common/units"
import "github.com/SpliceFS/common/wire"
```

## Building

Requires Go 1.26 or newer.

```
make check        # everything CI runs: format check, vet, tests with the race detector, coverage gate
make check-linux  # the same on Linux, in a container (needs Docker)
make lint         # golangci-lint
```

## Versioning

This module follows [Semantic Versioning](https://semver.org). Releases are git tags of the form
`vMAJOR.MINOR.PATCH`. A change to the JSON that `wire` describes is a change to the HTTP API,
which is specified in the
[design document of the server](https://github.com/SpliceFS/server/blob/main/docs/design.md).
Before 1.0.0 the packages may change in any minor release; every change is listed in the
[changelog](CHANGELOG.md).

## License

MIT. See [LICENSE](LICENSE).
