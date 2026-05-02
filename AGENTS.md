# Repository Guidelines

## Project Structure & Module Organization

Kekkai is a Go CLI for file integrity monitoring. The entry point is `cmd/kekkai/main.go`. Internal packages live under `internal/`: `cli` handles flags and command flow, `hash` performs file hashing and cache-aware verification, `cache` stores metadata with platform-specific time helpers, `manifest` generates and verifies manifests, `output` formats results, and `storage` contains S3 integration. Tests are colocated with packages as `*_test.go`; fixtures are under each package's `testdata/` directory.

## Build, Test, and Development Commands

- `make` builds `bin/kekkai` from the CLI entry point.
- `go build -o ./bin/kekkai ./cmd/kekkai` is the direct build equivalent.
- `make test` runs `go test -cover -count 1 ./...` for all packages.
- `go test -v ./internal/hash` runs one package's tests during focused work.
- `make vet` runs `go vet ./...`.
- `make staticcheck` runs `staticcheck -checks="all,-ST1000" ./...`.
- `make clean` removes generated binaries from `bin/`.

## Coding Style & Naming Conventions

Use standard Go formatting via `gofmt`; keep imports managed by `go mod tidy` when dependencies change. Prefer descriptive names, especially for errors such as `readErr` or `writeErr`, instead of ambiguous abbreviations. Pass `context.Context` through function parameters and create root contexts only near the CLI/main layer. Wrap errors with useful context using `fmt.Errorf("...: %w", err)` and prefer `errors.Is` / `errors.As` for inspection.

## Testing Guidelines

Use Go's standard `testing` package. Name tests `Test...` and keep package fixtures in `testdata/`. Add or update tests when changing CLI behavior, manifest verification, hashing, caching, symlink handling, output formats, or S3-facing logic. Cache and filesystem timestamp tests may need short sleeps to make `mtime`/`ctime` changes observable. Run `make test` before submitting changes; run `make vet` and `make staticcheck` for broader checks.

## Commit & Pull Request Guidelines

Recent history uses Conventional Commit-style subjects such as `fix(deps): update ...` and `chore(deps): update ...`; keep new commit subjects short, imperative, and scoped where useful. Pull requests should describe the behavior change, note security or performance implications, link related issues, and include test results. For CLI changes, update `README.md` examples or option documentation in the same PR.

## Security & Configuration Tips

Treat exclude rules, symlink handling, cache validation, and S3 access separation as security-sensitive. Deploy-time servers should have write access to manifests; application servers should have read-only access. Do not weaken cache ownership or permission checks without documenting the risk.
