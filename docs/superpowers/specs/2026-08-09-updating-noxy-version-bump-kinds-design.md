# Updating Noxy Version Bump Kinds Design

## Goal

Allow the repository-local `$updating-noxy-version` skill to update Noxy by semantic-version component instead of requiring a concrete version every time. When a user asks only to update or bump the version, default to a minor bump.

## Supported requests

The workflow accepts four target forms:

| Request | Current `1.5.0` becomes |
|---|---|
| `major` | `2.0.0` |
| `minor` | `1.6.0` |
| `patch` | `1.5.1` |
| `X.Y.Z` or `vX.Y.Z` | The explicit version |

Natural-language requests such as “update the version” or “bump Noxy” resolve to `minor` unless the user names `major`, `minor`, `patch`, or an explicit semantic version.

## Updater interface

Keep the existing positional interface and extend its accepted values:

```powershell
python .agents/skills/updating-noxy-version/scripts/update_version.py [major|minor|patch|X.Y.Z|vX.Y.Z] [--date YYYY-MM-DD] [--dry-run] [--root PATH]
```

The positional target becomes optional and defaults to `minor`. Explicit versions remain backward compatible.

The updater must derive bump targets from the single current version returned by the existing cross-surface consistency validation:

- `major`: increment major; reset minor and patch to zero.
- `minor`: preserve major; increment minor; reset patch to zero.
- `patch`: preserve major and minor; increment patch.
- Explicit version: preserve the current strict SemVer and monotonic-increase behavior.

All derived targets continue through the existing replacement, changelog, dry-run, transactional write, rollback, and recovery logic.

## Skill workflow and metadata

Update `SKILL.md` to resolve the user's request before preview:

1. Use an explicit version when supplied.
2. Otherwise use a named `major`, `minor`, or `patch` bump.
3. Default to `minor` when the request does not name a target.
4. Pass the resolved target directly to the bundled updater for dry-run and apply.

Replace the hardcoded `v1.6.0` UI prompt with a generic prompt that states the default-minor behavior and mentions major and patch overrides.

## Validation and errors

- Accept bump keywords case-insensitively after trimming surrounding whitespace.
- Reject any other target with an error listing the accepted keywords and explicit SemVer forms.
- Keep explicit SemVer restricted to ASCII digits.
- Preserve the rule that explicit target versions must be greater than the current version.
- A validation error or dry run must leave every release file unchanged.

## Testing

Add RED/GREEN regression coverage for:

- Default omitted target resolves to a minor bump.
- Named `major`, `minor`, and `patch` calculations.
- Case-insensitive and whitespace-trimmed bump keywords.
- Explicit `X.Y.Z` and `vX.Y.Z` compatibility.
- Invalid target rejection without writes.
- Derived bumps update exactly the same five surfaces and preserve history/dependencies.
- CLI argument parsing defaults the omitted target to `minor`.

Re-run the complete updater suite, skill validator, real repository dry run, Go tests/build/vet, and the Noxy integration suite before updating the open pull request.
