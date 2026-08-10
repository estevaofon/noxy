---
name: updating-noxy-version
description: Use when changing, bumping, releasing, or synchronizing the Noxy VM semantic version by major, minor, patch, or explicit target across runtime metadata, noxy.mod, README, language specification, and changelog.
---

# Updating Noxy Version

## Overview

Use the bundled updater from the Noxy repository root. Preserve unrelated user changes and stop on any validation or test failure.

## Workflow

1. Inspect `git status --short`. Note pre-existing changes, especially in the five release files.
2. Resolve the target:
   - Use an explicit `X.Y.Z` or `vX.Y.Z` when supplied.
   - Otherwise use the requested `major`, `minor`, or `patch` component.
   - Default to `minor` when the request does not name a target.
   - For an intentional migration to a lower explicit version, add
     `--allow-downgrade`; never use it for named component bumps.
3. Preview changes:
   `python .agents/skills/updating-noxy-version/scripts/update_version.py [target] [--allow-downgrade] --dry-run`
   Omit `[target]` to use the default minor bump. Add `--date YYYY-MM-DD` only when the user specifies a release date.
4. Review the preview. It must touch only the five expected release files.
5. Apply by rerunning the same command without `--dry-run`.
6. Review `git diff` and confirm historical changelog and dependency versions remain unchanged.
7. Run every verification command below. Do not skip failures.
8. Report the new version, changed files, and exact verification results.

## Required verification

| Check | Command |
|---|---|
| CLI version | `go run cmd/noxy/main.go --version` |
| Internal tests | `go test ./internal/...` |
| Integration suite | `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx` |
| Patch integrity | `git diff --check` |

The CLI output must equal `Noxy vX.Y.Z`, and the integration report must contain zero failures.

## Failure handling

- If the updater reports inconsistent or missing surfaces, inspect the files and report the conflict. Do not bypass the check with manual broad replacements.
- If a target release heading already exists, do not create a duplicate.
- If any verification fails, report the actual failure and leave the changes available for diagnosis.
- Do not commit, tag, push, or open a pull request unless the user separately requests it.

## Common mistakes

- Changing historical `CHANGELOG.md` entries or dependency versions.
- Updating only the runtime constant and leaving docs or `noxy.mod` stale.
- Claiming completion after a dry run or partial test suite.
