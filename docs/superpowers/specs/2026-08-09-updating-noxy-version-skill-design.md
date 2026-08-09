# Updating Noxy Version Skill Design

## Goal

Create a repository-local skill that updates the Noxy release version consistently, promotes the changelog, and runs the project's required verification without committing, tagging, or opening a pull request.

## Location and structure

Create the skill at `.agents/skills/updating-noxy-version/` with:

- `SKILL.md` for trigger metadata and the release workflow.
- `agents/openai.yaml` for discoverable UI metadata.
- `scripts/update_version.py` for deterministic validation and file updates.

No assets or separate reference documents are needed.

## Inputs and scope

The updater accepts a target semantic version as either `1.6.0` or `v1.6.0` and normalizes it to both plain and `v`-prefixed forms. It accepts an optional release date in `YYYY-MM-DD` format and otherwise uses the current local date. A `--dry-run` mode reports the proposed changes without writing files.

The script updates exactly these release surfaces:

| File | Expected value |
|---|---|
| `internal/version/version.go` | `const Version = "vX.Y.Z"` |
| `noxy.mod` | `noxy vX.Y.Z` |
| `README.md` | `Noxy REPL vX.Y.Z` |
| `docs/NOXY_LANGUAGE_SPEC.md` | `*Version: X.Y.Z*` |
| `CHANGELOG.md` | `## [X.Y.Z] - YYYY-MM-DD` below `## [Unreleased]` |

Historical changelog entries and dependency versions are not modified.

## Validation and write behavior

Before writing, the script reads all target files and validates:

1. The requested version is valid semantic version syntax.
2. The requested version is greater than the current Noxy version.
3. All active version surfaces agree on the current version.
4. Every expected replacement occurs exactly once.
5. The target changelog release heading does not already exist.

All new file contents are computed before the first write. A validation failure leaves every file unchanged and returns a nonzero exit code. Unrelated content in target files is preserved.

## Skill workflow

The skill instructs the agent to:

1. Inspect `git status` and preserve unrelated user changes.
2. Run the updater in `--dry-run` mode.
3. Review the proposed target files and then apply the update.
4. Review `git diff` and confirm no stale active version remains.
5. Run `go run cmd/noxy/main.go --version` and require the requested output.
6. Run `go test ./internal/...`.
7. Run `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx`.
8. Run `git diff --check` and report the changed files and verification results.

The workflow stops on any updater or verification failure. It does not commit, tag, push, or create a pull request.

## Testing strategy

Skill development follows RED-GREEN-REFACTOR:

- Run a baseline version-bump scenario without the new skill and record omissions or inconsistent choices.
- Test the updater against temporary repository fixtures for valid updates, dry-run behavior, invalid versions, non-increasing versions, inconsistent current versions, missing patterns, and duplicate changelog releases.
- Validate `SKILL.md` and `agents/openai.yaml` with the skill-authoring validation tools.
- Re-run the scenario with the new skill and confirm it selects the script, preserves scope, and names every required verification command.
- Exercise a real dry run against the Noxy repository without changing its current version.

## Error reporting

Errors identify the invalid input or exact file/pattern that blocked the operation. The agent reports the failure without claiming completion and does not bypass failed tests or consistency checks.
