# Desloppify Go detector triage

Desloppify 1.0 runs several language-agnostic heuristics that do not yet model
all Go package semantics. The repository tracks only the narrowly scoped
suppressions in `.desloppify/config.json`; scan state, review packets, and plans
remain local and ignored.

These suppressions prevent detector noise from encouraging production changes
that would make the Go design worse. They are not substitutes for `go test`,
coverage profiles, `go vet`, or `golangci-lint`.

## Suppression evidence

| Detector | Suppressed signal | Verified reason |
| --- | --- | --- |
| `unused` | Three imports of `github.com/vbauerster/mpb/v8` | `/v8` is the module's semantic import version. The imported package declares the identifier `mpb`, which all three files use. |
| `signature` | Variance among unrelated `Close` and `Start` methods | Go method identity includes its receiver. The reported methods belong to unrelated server, lifecycle, pipeline, store, WebSocket, and test-body types with intentionally different contracts. |
| `test_coverage` | Seventeen files reported as wholly untested | A fresh `go test ./... -coverprofile=coverage.out` executes 50.0%-100.0% of statements in every suppressed file. The static detector only maps same-basename `_test.go` files for Go, while these same-package tests are organized by behavior. |
| `flat_dirs` | `internal/downloader` | The detector combines 9 production files with 19 colocated Go test files and reports 28 files. Production ownership alone is below its 20-file threshold. |
| `flat_dirs` | `internal/server` | The detector combines 23 production files with 27 colocated tests. The package intentionally shares unexported server, store, job, and lifecycle state; threshold-only subpackages would force exports or dependency cycles without creating a real domain boundary. |

The genuinely untested `cmd/impartus/main.go` entrypoint remains unsuppressed.
The documentation generator now has 90.1% statement coverage and is no longer
reported by the scanner. Partial coverage remains visible in the Go coverage
report even when Desloppify's inaccurate whole-file finding is suppressed.

The three `unused` entries use Desloppify's exact line-based IDs rather than a
file wildcard. An import-line move therefore fails open and requires fresh
triage. Path-specific coverage and directory entries intentionally omit
Desloppify's path-independent fingerprints so that a moved or newly created
finding is not silently hidden. The two signature entries retain fingerprints
because each detector result is one aggregate whose representative file may
change while the same receiver evidence remains.

## Upstream detector reports

- [#640: Go unused-import package identifier](https://github.com/peteromallet/desloppify/issues/640)
- [#641: Go receiver-scoped signature variance](https://github.com/peteromallet/desloppify/issues/641)
- [#642: Go same-package coverage mapping](https://github.com/peteromallet/desloppify/issues/642)
- [#643: Go flat-directory test counting](https://github.com/peteromallet/desloppify/issues/643)

Re-evaluate the corresponding local suppressions after an upgraded Desloppify
release fixes and verifies each upstream issue. A fix for #643 should eliminate
the downloader's false count. The server suppression remains a documented Go
ownership-policy decision unless a real subpackage boundary emerges, because
its 23 production files independently exceed the generic threshold.

## Revalidation

Before adding, widening, or removing a suppression:

1. Run the full Go suite and collect real statement coverage:

   ```bash
   go test ./... -coverprofile=coverage.out
   go tool cover -func=coverage.out
   ```

2. Install the pinned project-local scanner and the same project-local
   golangci-lint used in CI:

   ```bash
   make quality-gate-install
   GOBIN="$PWD/.venv-desloppify/bin" \
     go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.1
   ```

3. Run the authoritative gate through the committed adapter and full-state
   completeness check:

   ```bash
   DESLOPPIFY_GOLANGCI_LINT_REAL="$PWD/.venv-desloppify/bin/golangci-lint" \
     make quality-gate
   ```

   `make quality-gate-scan` invokes Desloppify directly and is diagnostic-only;
   it does not inject the golangci-lint v2 adapter or enforce scan completeness.

4. Inspect every suppressed detector result against the implementation. Do not
   broaden a path-specific pattern merely to improve the score.

Desloppify 1.0 may materialize its default operational keys when it reads the
minimal tracked config. Treat those additions as local scanner state and do not
commit them unless they become intentional repository policy.

If a new production file is truly untested or a package gains a real ownership
boundary, address that finding rather than extending an existing suppression.
