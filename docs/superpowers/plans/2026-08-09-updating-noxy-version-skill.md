# Updating Noxy Version Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a repository-local skill that safely updates every active Noxy version surface, promotes the changelog, and runs the required verification without committing or tagging releases.

**Architecture:** A concise `SKILL.md` owns the agent workflow while a bundled, standard-library Python script owns deterministic validation and edits. The updater computes every changed file before writing, supports dry runs, and is tested against temporary repository fixtures.

**Tech Stack:** Markdown skill instructions, Python 3 standard library, `unittest`, Go project verification, Codex skill-authoring utilities.

## Global Constraints

- Install the skill at `.agents/skills/updating-noxy-version/`.
- Accept target versions as `X.Y.Z` or `vX.Y.Z`; persist the runtime and module forms with `v` and documentation forms without it where specified.
- Update only `internal/version/version.go`, `noxy.mod`, `README.md`, `docs/NOXY_LANGUAGE_SPEC.md`, and `CHANGELOG.md`.
- Preserve historical changelog entries, dependency versions, unrelated file content, and unrelated user changes.
- Validate all inputs and replacements before the first write; a validation failure must leave every file unchanged.
- Support `--date YYYY-MM-DD`, defaulting to the current local date, and `--dry-run`.
- Verification must run `go run cmd/noxy/main.go --version`, `go test ./internal/...`, `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx`, and `git diff --check`.
- The skill must not commit, tag, push, or open a pull request.

---

### Task 1: Capture the baseline behavior without the skill

**Files:**
- Read: `docs/superpowers/specs/2026-08-09-updating-noxy-version-skill-design.md`
- Read: `AGENTS.md`

**Interfaces:**
- Consumes: The approved design and existing Noxy release files.
- Produces: A baseline transcript identifying manual repetition or omissions that the skill must eliminate.

- [ ] **Step 1: Dispatch a fresh-context baseline scenario without exposing the new skill design**

Use a subagent with this exact prompt and do not give it access to the planned skill:

```text
In the Noxy repository, explain exactly how you would update the product from v1.5.0 to v1.6.0, promote the pending changelog with date 2026-08-09, preserve historical and dependency versions, and verify the result. Do not modify files, commit, tag, push, or create a PR. Return the files, edit rules, commands, and failure handling you would use.
```

- [ ] **Step 2: Record and classify the baseline result**

Check the transcript against this contract:

```text
Required active surfaces:
- internal/version/version.go
- noxy.mod
- README.md
- docs/NOXY_LANGUAGE_SPEC.md
- CHANGELOG.md

Required checks:
- semantic version syntax and monotonic increase
- agreement of all current active surfaces
- no duplicate release heading
- all replacements validated before writes
- dry-run support
- go run cmd/noxy/main.go --version
- go test ./internal/...
- go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
- git diff --check
```

Expected: the control cannot use a deterministic repository updater and therefore cannot guarantee an atomic prevalidated dry run. Record any additional omissions verbatim; use them to tighten Task 3 without expanding the approved scope.

---

### Task 2: Build the deterministic version updater with tests first

**Files:**
- Create: `.agents/skills/updating-noxy-version/SKILL.md`
- Create: `.agents/skills/updating-noxy-version/agents/openai.yaml`
- Create: `.agents/skills/updating-noxy-version/scripts/test_update_version.py`
- Create: `.agents/skills/updating-noxy-version/scripts/update_version.py`

**Interfaces:**
- Consumes: Repository root, target version string, optional release date, and dry-run flag.
- Produces: `normalize_version(value: str) -> tuple[str, tuple[int, int, int]]` and `execute_update(root: Path, target: str, release_date: str | None, dry_run: bool) -> str`.

- [ ] **Step 1: Initialize the skill scaffold with official tooling**

Run from the repository root:

```powershell
python 'C:/Users/estev/.codex/skills/.system/skill-creator/scripts/init_skill.py' updating-noxy-version --path .agents/skills --resources scripts --interface 'display_name=Update Noxy Version' --interface 'short_description=Update and validate Noxy release versions' --interface 'default_prompt=Use $updating-noxy-version to update Noxy to v1.6.0 and run all required checks.'
```

Expected: `.agents/skills/updating-noxy-version/` contains `SKILL.md`, `agents/openai.yaml`, and an empty `scripts/` directory.

- [ ] **Step 2: Write the updater tests before the updater exists**

Create `.agents/skills/updating-noxy-version/scripts/test_update_version.py` with:

```python
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from update_version import UpdateError, execute_update, normalize_version


TARGET_FILES = (
    "internal/version/version.go",
    "noxy.mod",
    "README.md",
    "docs/NOXY_LANGUAGE_SPEC.md",
    "CHANGELOG.md",
)


def create_fixture(root: Path, version: str = "1.5.0") -> None:
    contents = {
        "internal/version/version.go": (
            f'package version\n\nconst Version = "v{version}"\n'
        ),
        "noxy.mod": (
            f"module noxy\n\nnoxy v{version}\n\n"
            "require example.org/library v1.4.0\n"
        ),
        "README.md": f"# Noxy\n\n```noxy\nNoxy REPL v{version}\n```\n",
        "docs/NOXY_LANGUAGE_SPEC.md": (
            f"# Language\n\n---\n*Version: {version}*\n*Language: Noxy*\n"
        ),
        "CHANGELOG.md": (
            "# Changelog\n\n## [Unreleased]\n\n### Added\n\n"
            "- Pending feature.\n\n## [1.5.0] - 2026-08-08\n\n"
            "- Previous release.\n\n## [1.4.0] - 2026-08-01\n"
        ),
    }
    for relative, content in contents.items():
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content.encode("utf-8"))


def snapshot(root: Path) -> dict[str, bytes]:
    return {relative: (root / relative).read_bytes() for relative in TARGET_FILES}


class UpdateVersionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.root = Path(self.temp_dir.name)
        create_fixture(self.root)

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_normalizes_prefixed_and_plain_semver(self) -> None:
        self.assertEqual(normalize_version("1.6.0"), ("1.6.0", (1, 6, 0)))
        self.assertEqual(normalize_version("v1.6.0"), ("1.6.0", (1, 6, 0)))

    def test_updates_all_surfaces_and_preserves_history_and_dependencies(self) -> None:
        diff = execute_update(self.root, "v1.6.0", "2026-08-09", False)

        self.assertIn('const Version = "v1.6.0"', (self.root / TARGET_FILES[0]).read_text())
        self.assertIn("noxy v1.6.0", (self.root / TARGET_FILES[1]).read_text())
        self.assertIn("require example.org/library v1.4.0", (self.root / TARGET_FILES[1]).read_text())
        self.assertIn("Noxy REPL v1.6.0", (self.root / TARGET_FILES[2]).read_text())
        self.assertIn("*Version: 1.6.0*", (self.root / TARGET_FILES[3]).read_text())
        changelog = (self.root / TARGET_FILES[4]).read_text()
        self.assertIn("## [1.6.0] - 2026-08-09", changelog)
        self.assertIn("## [1.4.0] - 2026-08-01", changelog)
        self.assertIn("a/internal/version/version.go", diff)

    def test_dry_run_returns_diff_without_writing(self) -> None:
        before = snapshot(self.root)

        diff = execute_update(self.root, "1.6.0", "2026-08-09", True)

        self.assertEqual(snapshot(self.root), before)
        for relative in TARGET_FILES:
            self.assertIn(relative.replace("\\", "/"), diff)

    def test_rejects_invalid_semver_and_date(self) -> None:
        with self.assertRaisesRegex(UpdateError, "semantic version"):
            normalize_version("1.6")
        with self.assertRaisesRegex(UpdateError, "release date"):
            execute_update(self.root, "1.6.0", "09-08-2026", False)

    def test_rejects_equal_and_lower_versions_without_writing(self) -> None:
        for target in ("1.5.0", "1.4.9"):
            with self.subTest(target=target):
                before = snapshot(self.root)
                with self.assertRaisesRegex(UpdateError, "greater than current"):
                    execute_update(self.root, target, "2026-08-09", False)
                self.assertEqual(snapshot(self.root), before)

    def test_rejects_inconsistent_current_versions_without_writing(self) -> None:
        readme = self.root / "README.md"
        readme.write_text(readme.read_text().replace("v1.5.0", "v1.4.0"))
        before = snapshot(self.root)

        with self.assertRaisesRegex(UpdateError, "inconsistent current versions"):
            execute_update(self.root, "1.6.0", "2026-08-09", False)

        self.assertEqual(snapshot(self.root), before)

    def test_rejects_missing_surface_without_writing(self) -> None:
        spec = self.root / "docs/NOXY_LANGUAGE_SPEC.md"
        spec.write_text(spec.read_text().replace("*Version: 1.5.0*\n", ""))
        before = snapshot(self.root)

        with self.assertRaisesRegex(UpdateError, "expected exactly one active version"):
            execute_update(self.root, "1.6.0", "2026-08-09", False)

        self.assertEqual(snapshot(self.root), before)

    def test_rejects_duplicate_target_release_without_writing(self) -> None:
        changelog = self.root / "CHANGELOG.md"
        changelog.write_text(
            changelog.read_text() + "\n## [1.6.0] - 2026-07-01\n"
        )
        before = snapshot(self.root)

        with self.assertRaisesRegex(UpdateError, "already exists"):
            execute_update(self.root, "1.6.0", "2026-08-09", False)

        self.assertEqual(snapshot(self.root), before)


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 3: Run the tests and verify RED**

Run:

```powershell
python -m unittest .agents/skills/updating-noxy-version/scripts/test_update_version.py -v
```

Expected: ERROR with `ModuleNotFoundError: No module named 'update_version'`. The failure must be caused by the missing updater, not by a syntax error in the test.

- [ ] **Step 4: Implement the minimal updater**

Create `.agents/skills/updating-noxy-version/scripts/update_version.py` with:

```python
#!/usr/bin/env python3
import argparse
import difflib
import re
import sys
from datetime import date
from pathlib import Path


SEMVER = r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)"
TARGET_VERSION = re.compile(rf"^v?(?P<version>{SEMVER})$")
RELEASE_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
RELEASE_HEADING = re.compile(
    rf"(?m)^## \[(?P<version>{SEMVER})\] - \d{{4}}-\d{{2}}-\d{{2}}\r?$"
)
SURFACES = (
    (
        "internal/version/version.go",
        re.compile(rf'(?m)^const Version = "v(?P<version>{SEMVER})"(?=\r?$)'),
        lambda version: f'const Version = "v{version}"',
    ),
    (
        "noxy.mod",
        re.compile(rf"(?m)^noxy v(?P<version>{SEMVER})(?=\r?$)"),
        lambda version: f"noxy v{version}",
    ),
    (
        "README.md",
        re.compile(rf"(?m)^Noxy REPL v(?P<version>{SEMVER})(?=\r?$)"),
        lambda version: f"Noxy REPL v{version}",
    ),
    (
        "docs/NOXY_LANGUAGE_SPEC.md",
        re.compile(rf"(?m)^\*Version: (?P<version>{SEMVER})\*(?=\r?$)"),
        lambda version: f"*Version: {version}*",
    ),
)
CHANGELOG = "CHANGELOG.md"
TARGET_FILES = tuple(item[0] for item in SURFACES) + (CHANGELOG,)


class UpdateError(ValueError):
    pass


def normalize_version(value: str) -> tuple[str, tuple[int, int, int]]:
    match = TARGET_VERSION.fullmatch(value.strip())
    if not match:
        raise UpdateError(f"invalid semantic version: {value!r}; expected X.Y.Z or vX.Y.Z")
    normalized = match.group("version")
    return normalized, tuple(int(part) for part in normalized.split("."))


def normalize_date(value: str | None) -> str:
    normalized = value or date.today().isoformat()
    if not RELEASE_DATE.fullmatch(normalized):
        raise UpdateError(f"invalid release date: {normalized!r}; expected YYYY-MM-DD")
    try:
        date.fromisoformat(normalized)
    except ValueError as error:
        raise UpdateError(f"invalid release date: {normalized!r}") from error
    return normalized


def read_targets(root: Path) -> dict[str, str]:
    contents = {}
    for relative in TARGET_FILES:
        path = root / relative
        try:
            contents[relative] = path.read_bytes().decode("utf-8")
        except FileNotFoundError as error:
            raise UpdateError(f"required file not found: {relative}") from error
        except UnicodeDecodeError as error:
            raise UpdateError(f"required file is not UTF-8: {relative}") from error
    return contents


def inspect_current_versions(contents: dict[str, str]) -> str:
    current = []
    for relative, pattern, _replacement in SURFACES:
        matches = list(pattern.finditer(contents[relative]))
        if len(matches) != 1:
            raise UpdateError(
                f"{relative}: expected exactly one active version, found {len(matches)}"
            )
        current.append((relative, matches[0].group("version")))

    releases = list(RELEASE_HEADING.finditer(contents[CHANGELOG]))
    if not releases:
        raise UpdateError(f"{CHANGELOG}: expected at least one dated release heading")
    current.append((CHANGELOG, releases[0].group("version")))

    versions = {version for _relative, version in current}
    if len(versions) != 1:
        details = ", ".join(f"{relative}={version}" for relative, version in current)
        raise UpdateError(f"inconsistent current versions: {details}")
    return current[0][1]


def build_updates(
    contents: dict[str, str], target: str, release_date: str
) -> dict[str, str]:
    updated = {}
    for relative, pattern, replacement in SURFACES:
        value, count = pattern.subn(lambda _match: replacement(target), contents[relative])
        if count != 1:
            raise UpdateError(f"{relative}: expected exactly one replacement, found {count}")
        updated[relative] = value

    changelog = contents[CHANGELOG]
    if any(match.group("version") == target for match in RELEASE_HEADING.finditer(changelog)):
        raise UpdateError(f"{CHANGELOG}: release {target} already exists")
    newline = "\r\n" if "\r\n" in changelog else "\n"
    marker = "## [Unreleased]"
    if changelog.splitlines().count(marker) != 1:
        raise UpdateError(f"{CHANGELOG}: expected exactly one {marker} heading")
    anchor = marker + newline
    if anchor not in changelog:
        raise UpdateError(f"{CHANGELOG}: {marker} heading must end with a newline")
    heading = f"## [{target}] - {release_date}"
    updated[CHANGELOG] = changelog.replace(
        anchor, anchor + newline + heading + newline, 1
    )
    return updated


def render_diff(original: dict[str, str], updated: dict[str, str]) -> str:
    chunks = []
    for relative in TARGET_FILES:
        chunks.extend(
            difflib.unified_diff(
                original[relative].splitlines(keepends=True),
                updated[relative].splitlines(keepends=True),
                fromfile=f"a/{relative}",
                tofile=f"b/{relative}",
            )
        )
    return "".join(chunks)


def execute_update(
    root: Path, target: str, release_date: str | None = None, dry_run: bool = False
) -> str:
    root = root.resolve()
    normalized, target_tuple = normalize_version(target)
    normalized_date = normalize_date(release_date)
    original = read_targets(root)
    current = inspect_current_versions(original)
    _current_text, current_tuple = normalize_version(current)
    if target_tuple <= current_tuple:
        raise UpdateError(
            f"target version {normalized} must be greater than current version {current}"
        )

    updated = build_updates(original, normalized, normalized_date)
    diff = render_diff(original, updated)
    if not dry_run:
        for relative in TARGET_FILES:
            (root / relative).write_bytes(updated[relative].encode("utf-8"))
    return diff


def default_root() -> Path:
    return Path(__file__).resolve().parents[4]


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Update all active Noxy release version surfaces."
    )
    parser.add_argument("version", help="target semantic version, with optional v prefix")
    parser.add_argument("--date", dest="release_date", help="release date as YYYY-MM-DD")
    parser.add_argument("--dry-run", action="store_true", help="print diff without writing")
    parser.add_argument("--root", type=Path, default=default_root(), help="Noxy repository root")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        diff = execute_update(args.root, args.version, args.release_date, args.dry_run)
    except UpdateError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    print(diff, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
```

- [ ] **Step 5: Run the updater tests and verify GREEN**

Run:

```powershell
python -m unittest .agents/skills/updating-noxy-version/scripts/test_update_version.py -v
```

Expected: `Ran 8 tests` followed by `OK`.

- [ ] **Step 6: Commit the tested updater**

```powershell
git add -- .agents/skills/updating-noxy-version/scripts/test_update_version.py .agents/skills/updating-noxy-version/scripts/update_version.py
git commit -m "feat: add deterministic noxy version updater"
```

---

### Task 3: Write the skill workflow and metadata

**Files:**
- Modify: `.agents/skills/updating-noxy-version/SKILL.md`
- Verify: `.agents/skills/updating-noxy-version/agents/openai.yaml`

**Interfaces:**
- Consumes: `scripts/update_version.py VERSION [--date YYYY-MM-DD] [--dry-run] [--root PATH]`.
- Produces: A discoverable `$updating-noxy-version` workflow that invokes the updater and all required project checks.

- [ ] **Step 1: Replace the generated skill template with the minimal workflow**

Write `.agents/skills/updating-noxy-version/SKILL.md` as:

```markdown
---
name: updating-noxy-version
description: Use when changing, bumping, releasing, or synchronizing the Noxy VM semantic version across runtime metadata, noxy.mod, README, language specification, and changelog.
---

# Updating Noxy Version

## Overview

Use the bundled updater from the Noxy repository root. Preserve unrelated user changes and stop on any validation or test failure.

## Workflow

1. Inspect `git status --short`. Note pre-existing changes, especially in the five release files.
2. Normalize the requested target to `vX.Y.Z` for reporting.
3. Preview changes:
   `python .agents/skills/updating-noxy-version/scripts/update_version.py <version> --dry-run`
   Add `--date YYYY-MM-DD` only when the user specifies a release date.
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
```

- [ ] **Step 2: Verify the generated UI metadata exactly matches the skill**

`.agents/skills/updating-noxy-version/agents/openai.yaml` must contain:

```yaml
interface:
  display_name: "Update Noxy Version"
  short_description: "Update and validate Noxy release versions"
  default_prompt: "Use $updating-noxy-version to update Noxy to v1.6.0 and run all required checks."
```

If the generated file differs, regenerate it:

```powershell
python 'C:/Users/estev/.codex/skills/.system/skill-creator/scripts/generate_openai_yaml.py' .agents/skills/updating-noxy-version --interface 'display_name=Update Noxy Version' --interface 'short_description=Update and validate Noxy release versions' --interface 'default_prompt=Use $updating-noxy-version to update Noxy to v1.6.0 and run all required checks.'
```

- [ ] **Step 3: Validate the skill structure**

Run:

```powershell
python 'C:/Users/estev/.codex/skills/.system/skill-creator/scripts/quick_validate.py' .agents/skills/updating-noxy-version
```

Expected: `Skill is valid!`

- [ ] **Step 4: Check token efficiency and placeholders**

Run:

```powershell
(Get-Content -Raw .agents/skills/updating-noxy-version/SKILL.md | Measure-Object -Word).Words
rg -n 'TBD|FIXME|placeholder' .agents/skills/updating-noxy-version
```

Expected: fewer than 500 words and no placeholder matches.

- [ ] **Step 5: Commit the skill instructions and metadata**

```powershell
git add -- .agents/skills/updating-noxy-version/SKILL.md .agents/skills/updating-noxy-version/agents/openai.yaml
git commit -m "docs: add noxy version update skill"
```

---

### Task 4: Validate behavior end to end

**Files:**
- Test: `.agents/skills/updating-noxy-version/scripts/test_update_version.py`
- Test: `.agents/skills/updating-noxy-version/SKILL.md`
- Test: `.agents/skills/updating-noxy-version/agents/openai.yaml`

**Interfaces:**
- Consumes: The completed local skill and the current Noxy v1.5.0 repository.
- Produces: Fresh evidence that the updater is safe, the skill is valid, and a future agent follows the intended workflow.

- [ ] **Step 1: Re-run the complete updater test suite**

```powershell
python -m unittest .agents/skills/updating-noxy-version/scripts/test_update_version.py -v
```

Expected: `Ran 8 tests` and `OK`.

- [ ] **Step 2: Exercise a real repository dry run**

Capture `git status --short`, then run:

```powershell
python .agents/skills/updating-noxy-version/scripts/update_version.py v1.6.0 --date 2026-08-09 --dry-run
```

Expected: a unified diff for exactly the five release files, with no filesystem changes. Compare `git status --short` before and after; the output must be identical.

- [ ] **Step 3: Forward-test the skill with a fresh-context agent**

Use a subagent with this exact prompt:

```text
Use $updating-noxy-version at .agents/skills/updating-noxy-version to explain how you would update this Noxy repository from v1.5.0 to v1.6.0 with release date 2026-08-09. Do not modify files. Return the exact preview, apply, verification, and failure-handling workflow you would follow. Do not commit, tag, push, or create a PR.
```

Expected: the agent uses the bundled updater, previews before applying, names all four verification commands, preserves historical/dependency versions, stops on validation failures, and performs no release Git operations. Compare with Task 1 and add only guidance required to close observed gaps; re-run `quick_validate.py` after any edit.

- [ ] **Step 4: Run final structural and patch checks**

```powershell
python 'C:/Users/estev/.codex/skills/.system/skill-creator/scripts/quick_validate.py' .agents/skills/updating-noxy-version
git diff --check
git status --short
```

Expected: `Skill is valid!`, no patch errors, and only intentional task files differ from the task's starting state.

- [ ] **Step 5: Commit any forward-test refinements**

If Task 4 Step 3 required changes:

```powershell
git add -- .agents/skills/updating-noxy-version
git commit -m "docs: refine noxy version update workflow"
```

If no refinements were required, do not create an empty commit.
