# Contributing

Thank you for helping improve droidperm.

## Before opening a change

- Search existing issues and pull requests.
- Keep the change focused. droidperm intentionally manages only runtime
  permissions and AppOps through ADB.
- Open an issue before adding a new policy surface, dependency, or compatibility
  layer.
- Do not include device identifiers, package inventories, or other private data
  in logs and fixtures.

## Development

The project uses Go 1.25. A reproducible Nix development shell is also provided.

```sh
nix develop
go test ./...
go vet ./...
go build ./...
```

Tests should describe observable behavior. Parser changes should include
sanitized output fixtures for the relevant Android API level. Device-specific
workarounds need evidence, a narrow scope, and documentation in
`docs/compatibility.md`.

## Pull requests

1. Keep commits and the final diff small and reviewable.
2. Add or update tests for behavior changes.
3. Run the commands above.
4. Update `README.md`, `AGENTS.md`, `CLAUDE.md`, or `docs/` when the change
   affects their documented behavior. Skip documents that are unaffected.
5. Complete the pull request template.

By contributing, you agree that your contribution is licensed under the
Apache License 2.0.
