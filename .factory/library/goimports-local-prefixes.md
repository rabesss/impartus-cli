# Goimports Local Prefixes Configuration

## Summary

The `.golangci.yml` uses `local-prefixes: github.com/rabesss/impartus-cli` for goimports, which requires local packages to be grouped separately from third-party imports.

## Details

When running `goimports` directly, you must use:

```bash
goimports -local "github.com/rabesss/impartus-cli" -w <file>
```

Without the `-local` flag, `goimports` will organize imports differently than what `golangci-lint` expects based on the repo configuration, causing false-positive failures.

## Location

`.golangci.yml` line 44:
```yaml
local-prefixes: github.com/rabesss/impartus-cli
```

## Troubleshooting

If `make lint` reports goimports issues but `goimports -w <file>` appears to fix them:

1. Check if the issue is local-prefixes related: `golangci-lint run -v --no-config --disable-all --enable=goimports`
2. If that passes but `make lint` fails, use: `goimports -local "github.com/rabesss/impartus-cli" -w <file>`

## Discovery

This was discovered during `repo-wide-lint-debt-remediation` when isolated goimports passed but `make lint` failed on the same files.
