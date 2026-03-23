# CLAUDE.md

Project-level development instructions for gobrrr.

## Build

```bash
CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/
```

## Test

```bash
go test ./...
```

## Constraints

- Pure Go, no cgo (`CGO_ENABLED=0`)
- All JSON persistence uses atomic writes: write to `.tmp` file first, then `os.Rename` to the target path
- File permissions: secrets `0600`, directories `0700`

## Spec

The design spec is at `docs/specs/2026-03-23-gobrrr-design.md`.

## Key Conventions

- All subcommand stubs print "not implemented" until logic is wired
- Unix socket for daemon IPC
- Workers are spawned as `claude -p` processes
