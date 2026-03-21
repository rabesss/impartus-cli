# Module Guide: Config

## Scope
- Directory: `internal/config`
- Owned paths: `internal/config/**`

## Purpose
- Loads `config.json`, applies runtime defaults, validates constraints, and exposes shared config access.

## Interfaces / Contracts
- Core types: `Config`, `ProgressConfig`.
- Parse/load APIs: `Parse(path)`, `Load(path)`, and singleton accessor `Get()`.
- Lifecycle methods: `(*Config).ApplyDefaults()` then `(*Config).Validate()`.

## Data / Types
- Keep shared/public types centralized (avoid duplicate local shape drift).
- Prefer explicit types over `any` and implicit contracts.

## Local Quality Bar
- Keep files small and readable (target <= 300 lines per file).
- Avoid very large flat folders (target <= 20 direct files per folder).
- Add or update tests when behavior changes.
- Keep `BaseUrl` and `BaseURL` mirrored for compatibility with existing config files.
- Maintain current validation enums/ranges (quality, views, worker counts, rate limits, timeout bounds).
- New call paths should run `ApplyDefaults` before `Validate`.

## Agent Rules (Enforced)
- Read this file before editing files in this module.
- If behavior/interfaces/contracts change, update this file in the same change.
- Keep changes scoped to this module unless cross-module contract changes are required.
