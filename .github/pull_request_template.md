# Pull Request

## Conventional Commits

> ⚠️ **Your PR title must follow [Conventional Commits](https://www.conventionalcommits.org/).**
>
> - The PR title becomes the squash commit message on `main`.
> - `release-please` only sees the squash commit title, not the inner commits.

**Valid types:** `feat`, `fix`, `chore`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `revert`

**Valid scopes (optional):** `cli`, `api`, `downloader`, `config`, `server`, `ci`, `deps`, `security`, `lint`, `test`, `docs`

**Examples:**
- `feat(cli): add interactive download prompt`
- `fix(api): resolve race condition in job store`
- `chore(deps): bump gorilla/websocket to v1.5.3`
- `docs: update API reference for new endpoint`

## Description
<!-- Provide a clear and concise description of your changes -->

**What does this PR do?**
<!-- Explain the purpose of this change -->

**Related Issue(s):**
<!-- Link to any related issues: Fixes #123, Addresses #456 -->
-

## Type of Change
<!-- Mark the relevant option with an 'x' -->
- [ ] 🐛 Bug fix (non-breaking change that fixes an issue)
- [ ] ✨ New feature (non-breaking change that adds functionality)
- [ ] 💥 Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] 📚 Documentation update
- [ ] 🔧 Configuration/infrastructure change
- [ ] ♻️ Refactoring (no functional changes)

## Testing Done
<!-- Describe the testing you've done to validate your changes -->

**Unit Tests:**
- [ ] Added new tests
- [ ] Updated existing tests
- [ ] All tests pass locally (`go test ./...`)

**Manual Testing:**
<!-- Describe what you tested manually -->
```bash
# Commands you ran to test:
go test ./...
go build ./...
./impartus version
```

**Test Coverage:**
<!-- If applicable, note coverage impact -->
- Coverage before: N/A
- Coverage after: N/A

## Code Quality Checklist
<!-- Mark completed items with an 'x' -->
- [ ] Code follows the project's naming conventions (PascalCase/camelCase)
- [ ] No new linting errors (`golangci-lint run` passes)
- [ ] No hardcoded secrets or credentials
- [ ] Complex logic is documented with comments
- [ ] Error messages are clear and actionable

## Documentation
- [ ] README.md updated (if needed)
- [ ] docs/ updated (if API, CLI, or config behavior changed)
- [ ] Changelog entry added (if user-facing change)

## Screenshots (if applicable)
<!-- Add screenshots for UI-related changes -->

## Additional Context
<!-- Add any other context about the PR here -->

---
<!-- For maintainers: Review checklist -->
**Reviewer Checklist:**
- [ ] Code is readable and maintainable
- [ ] Tests are comprehensive and meaningful
- [ ] No security vulnerabilities introduced
- [ ] Backward compatible (or breaking change is documented)
