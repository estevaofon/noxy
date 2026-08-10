# Updating Noxy Version Bump Kinds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `$updating-noxy-version` so users can request `major`, `minor`, `patch`, or an explicit semantic version, with `minor` as the default when no target is supplied.

**Architecture:** Keep all version calculation in the bundled Python updater. Add a small target-resolution function that derives named bumps from the already validated current version, then reuse the existing monotonicity, five-surface replacement, dry-run, transactional write, rollback, and recovery pipeline. Update the skill workflow and UI prompt to expose the new natural-language behavior without a hardcoded release number.

**Tech Stack:** Python 3 standard library, `unittest`, Markdown skill instructions, Codex skill-authoring utilities, Go/Noxy project verification.

## Global Constraints

- Accept `major`, `minor`, `patch`, `X.Y.Z`, and `vX.Y.Z`.
- Default an omitted CLI target and an unspecified natural-language request to `minor`.
- Apply SemVer component rules: major resets minor/patch, minor resets patch, and patch preserves major/minor.
- Accept bump keywords case-insensitively after trimming surrounding whitespace.
- Keep explicit SemVer restricted to ASCII digits and require explicit targets to be greater than the current version.
- Update only `internal/version/version.go`, `noxy.mod`, `README.md`, `docs/NOXY_LANGUAGE_SPEC.md`, and `CHANGELOG.md`.
- Preserve history, dependency versions, unrelated content, transactional rollback, recovery artifacts, `--date`, `--dry-run`, and all existing public behavior.
- Keep the skill under 500 words and remove the hardcoded `v1.6.0` UI prompt.
- Do not apply a real Noxy version bump while implementing this feature.
- Update the existing pull request by pushing the current branch; do not open a second PR.

---

### Task 1: Add named and default bump targets to the updater

**Files:**
- Modify: `.agents/skills/updating-noxy-version/scripts/test_update_version.py`
- Modify: `.agents/skills/updating-noxy-version/scripts/update_version.py`

**Interfaces:**
- Consumes: the current version tuple returned by `normalize_version(current)`.
- Produces: `resolve_target(value: str, current: tuple[int, int, int]) -> tuple[str, tuple[int, int, int]]`.
- Preserves: `execute_update(root: Path, target: str, release_date: str | None = None, dry_run: bool = False) -> str`.
- Changes CLI: positional `version` becomes optional with default `minor`.

- [ ] **Step 1: Add imports and failing target-resolution tests**

Extend the updater import in `test_update_version.py`:

```python
from update_version import (
    UpdateError,
    execute_update,
    normalize_version,
    parse_args,
    resolve_target,
)
```

Add these test methods to `UpdateVersionTests`:

```python
def test_resolves_named_and_explicit_targets(self) -> None:
    current = (1, 5, 7)

    self.assertEqual(resolve_target("major", current), ("2.0.0", (2, 0, 0)))
    self.assertEqual(resolve_target("minor", current), ("1.6.0", (1, 6, 0)))
    self.assertEqual(resolve_target("patch", current), ("1.5.8", (1, 5, 8)))
    self.assertEqual(resolve_target(" MiNoR ", current), ("1.6.0", (1, 6, 0)))
    self.assertEqual(resolve_target("v2.3.4", current), ("2.3.4", (2, 3, 4)))

def test_named_minor_bump_updates_every_surface(self) -> None:
    diff = execute_update(self.root, "minor", "2026-08-09", False)

    self.assertIn('const Version = "v1.6.0"', (self.root / TARGET_FILES[0]).read_text())
    self.assertIn("noxy v1.6.0", (self.root / TARGET_FILES[1]).read_text())
    self.assertIn("require example.org/library v1.4.0", (self.root / TARGET_FILES[1]).read_text())
    self.assertIn("Noxy REPL v1.6.0", (self.root / TARGET_FILES[2]).read_text())
    self.assertIn("*Version: 1.6.0*", (self.root / TARGET_FILES[3]).read_text())
    changelog = (self.root / TARGET_FILES[4]).read_text()
    self.assertIn("## [1.6.0] - 2026-08-09", changelog)
    self.assertIn("## [1.4.0] - 2026-08-01", changelog)
    self.assertIn("a/internal/version/version.go", diff)

def test_rejects_invalid_target_without_writing(self) -> None:
    before = snapshot(self.root)

    with self.assertRaisesRegex(
        UpdateError, "major, minor, patch, X.Y.Z, or vX.Y.Z"
    ):
        execute_update(self.root, "feature", "2026-08-09", False)

    self.assertEqual(snapshot(self.root), before)

def test_cli_defaults_omitted_target_to_minor(self) -> None:
    args = parse_args([])

    self.assertEqual(args.version, "minor")
```

- [ ] **Step 2: Run the updater suite and verify RED**

Run:

```powershell
python .agents/skills/updating-noxy-version/scripts/test_update_version.py -v
```

Expected: import fails with `ImportError: cannot import name 'resolve_target'`. The failure must come from the missing target resolver.

- [ ] **Step 3: Add the minimal target resolver**

Add below `normalize_version` in `update_version.py`:

```python
def resolve_target(
    value: str, current: tuple[int, int, int]
) -> tuple[str, tuple[int, int, int]]:
    normalized = value.strip()
    kind = normalized.lower()
    major, minor, patch = current

    if kind == "major":
        target = (major + 1, 0, 0)
    elif kind == "minor":
        target = (major, minor + 1, 0)
    elif kind == "patch":
        target = (major, minor, patch + 1)
    else:
        try:
            return normalize_version(normalized)
        except UpdateError as error:
            raise UpdateError(
                f"invalid version target: {value!r}; expected "
                "major, minor, patch, X.Y.Z, or vX.Y.Z"
            ) from error

    return ".".join(str(part) for part in target), target
```

Do not add new dependencies or an unused keyword constant.

- [ ] **Step 4: Resolve the target after validating current surfaces**

Replace the beginning of `execute_update` through the monotonicity check with:

```python
def execute_update(
    root: Path, target: str, release_date: str | None = None, dry_run: bool = False
) -> str:
    root = root.resolve()
    normalized_date = normalize_date(release_date)
    original = read_targets(root)
    current = inspect_current_versions(original)
    _current_text, current_tuple = normalize_version(current)
    normalized, target_tuple = resolve_target(target, current_tuple)
    if target_tuple <= current_tuple:
        raise UpdateError(
            f"target version {normalized} must be greater than current version {current}"
        )

    updated = build_updates(original, normalized, normalized_date)
    diff = render_diff(original, updated)
    if not dry_run:
        _write_updates(root, original, updated)
    return diff
```

This ordering ensures named bumps are derived only from a current version that agrees across every active surface.

- [ ] **Step 5: Make the CLI target optional and default it to minor**

Replace the positional argument in `parse_args` with:

```python
parser.add_argument(
    "version",
    nargs="?",
    default="minor",
    help="target version: major, minor, patch, X.Y.Z, or vX.Y.Z (default: minor)",
)
```

Keep `main` passing `args.version` to `execute_update`.

- [ ] **Step 6: Run the updater suite and verify GREEN**

Run:

```powershell
python .agents/skills/updating-noxy-version/scripts/test_update_version.py -v
python -m py_compile .agents/skills/updating-noxy-version/scripts/update_version.py .agents/skills/updating-noxy-version/scripts/test_update_version.py
git diff --check
```

Expected: `Ran 17 tests`, `OK`, Python compilation exit 0, and no patch errors.

- [ ] **Step 7: Commit the updater behavior**

```powershell
git add -- .agents/skills/updating-noxy-version/scripts/update_version.py .agents/skills/updating-noxy-version/scripts/test_update_version.py
git commit -m "feat: support semantic version bump targets"
```

---

### Task 2: Update the skill workflow, UI prompt, and changelog

**Files:**
- Modify: `.agents/skills/updating-noxy-version/SKILL.md`
- Modify: `.agents/skills/updating-noxy-version/agents/openai.yaml`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: `update_version.py [major|minor|patch|X.Y.Z|vX.Y.Z] [--date YYYY-MM-DD] [--dry-run]`.
- Produces: natural-language default-minor behavior and generic UI metadata without a concrete release number.

- [ ] **Step 1: Capture the current-skill baseline before editing**

Dispatch a fresh-context agent with:

```text
Use $updating-noxy-version at .agents/skills/updating-noxy-version to explain the exact commands you would run for each request: "update the Noxy version", "bump the patch version", "bump the major version", and "update to v2.3.4". Do not modify files. Report the default used when no target is named.
```

Expected current failure: the skill requires a concrete `<version>`, has no default-minor rule, does not define named component commands, and its UI prompt hardcodes `v1.6.0`. Record the transcript in the ignored SDD workspace.

- [ ] **Step 2: Replace the target-resolution portion of SKILL.md**

Change the frontmatter description to:

```yaml
description: Use when changing, bumping, releasing, or synchronizing the Noxy VM semantic version by major, minor, patch, or explicit target across runtime metadata, noxy.mod, README, language specification, and changelog.
```

Replace Workflow steps 2 and 3 with:

```markdown
2. Resolve the target:
   - Use an explicit `X.Y.Z` or `vX.Y.Z` when supplied.
   - Otherwise use the requested `major`, `minor`, or `patch` component.
   - Default to `minor` when the request does not name a target.
3. Preview changes:
   `python .agents/skills/updating-noxy-version/scripts/update_version.py [target] --dry-run`
   Omit `[target]` to use the default minor bump. Add `--date YYYY-MM-DD` only when the user specifies a release date.
```

Keep the remaining preview review, apply, verification, failure handling, and no-release-Git-operation rules unchanged.

- [ ] **Step 3: Regenerate generic UI metadata**

Run:

```powershell
python 'C:/Users/estev/.codex/skills/.system/skill-creator/scripts/generate_openai_yaml.py' .agents/skills/updating-noxy-version --interface 'display_name=Update Noxy Version' --interface 'short_description=Update and validate Noxy release versions' --interface 'default_prompt=Use $updating-noxy-version to update Noxy''s minor version by default, or apply a major or patch bump when requested, and run all required checks.'
```

Verify `agents/openai.yaml` contains:

```yaml
interface:
  display_name: "Update Noxy Version"
  short_description: "Update and validate Noxy release versions"
  default_prompt: "Use $updating-noxy-version to update Noxy's minor version by default, or apply a major or patch bump when requested, and run all required checks."
```

- [ ] **Step 4: Refine the Unreleased changelog entry**

Replace the current updater bullet with:

```markdown
- Repository-local skill for safe, transactional Noxy version updates by major, minor, patch, or explicit target, with default-minor behavior, dry-run validation, and rollback coverage. `#tooling` @estevaofon
```

Do not create a dated release heading.

- [ ] **Step 5: Validate structure, size, metadata, and patch**

Run:

```powershell
python 'C:/Users/estev/.codex/skills/.system/skill-creator/scripts/quick_validate.py' .agents/skills/updating-noxy-version
(Get-Content -Raw .agents/skills/updating-noxy-version/SKILL.md | Measure-Object -Word).Words
rg -n 'TBD|TODO|FIXME|placeholder|v1\.6\.0' .agents/skills/updating-noxy-version/SKILL.md .agents/skills/updating-noxy-version/agents/openai.yaml
git diff --check
```

Expected: `Skill is valid!`, fewer than 500 words, no scan matches, and no patch errors.

- [ ] **Step 6: Forward-test the updated skill**

Dispatch a different fresh-context agent with the same Step 1 prompt.

Expected:

- “update the Noxy version” previews and applies with the omitted target or `minor`.
- Patch uses `patch`.
- Major uses `major`.
- Explicit `v2.3.4` remains supported.
- Every path previews before applying, names all four project checks, and performs no commit/tag/push/PR operation.

If guidance changes are required, edit only the minimum wording, rerun the official validator, and repeat the forward-test.

- [ ] **Step 7: Commit the workflow and metadata**

```powershell
git add -- .agents/skills/updating-noxy-version/SKILL.md .agents/skills/updating-noxy-version/agents/openai.yaml CHANGELOG.md
git commit -m "docs: generalize version update skill targets"
```

---

### Task 3: Validate end to end and update the existing PR

**Files:**
- Test: `.agents/skills/updating-noxy-version/scripts/test_update_version.py`
- Test: `.agents/skills/updating-noxy-version/SKILL.md`
- Test: `.agents/skills/updating-noxy-version/agents/openai.yaml`
- Verify: existing PR `https://github.com/estevaofon/noxy/pull/11`

**Interfaces:**
- Consumes: the completed updater, skill, and metadata.
- Produces: fresh local evidence and updated commits on the existing remote PR branch.

- [ ] **Step 1: Run the complete updater and skill checks**

```powershell
python .agents/skills/updating-noxy-version/scripts/test_update_version.py -v
python -m py_compile .agents/skills/updating-noxy-version/scripts/update_version.py .agents/skills/updating-noxy-version/scripts/test_update_version.py
python 'C:/Users/estev/.codex/skills/.system/skill-creator/scripts/quick_validate.py' .agents/skills/updating-noxy-version
git diff --check
```

Expected: `Ran 17 tests`, `OK`, successful compilation, `Skill is valid!`, and no patch errors.

- [ ] **Step 2: Exercise the default-minor real-repository dry run**

Capture `git status --short`, then run:

```powershell
python .agents/skills/updating-noxy-version/scripts/update_version.py --date 2026-08-09 --dry-run
```

Expected: exactly five diff headers and a derived target of `1.6.0` from the repository's current `1.5.0`. Compare status before and after; it must be identical.

- [ ] **Step 3: Exercise major and patch calculations without writes**

Run:

```powershell
python .agents/skills/updating-noxy-version/scripts/update_version.py major --date 2026-08-09 --dry-run
python .agents/skills/updating-noxy-version/scripts/update_version.py patch --date 2026-08-09 --dry-run
```

Expected: the major diff uses `2.0.0`, the patch diff uses `1.5.1`, both touch exactly five files, and `git status --short` remains unchanged.

- [ ] **Step 4: Run the project verification suite**

```powershell
go test ./...
go build ./...
go vet ./...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: every Go command exits 0 and the integration report contains `Falhou: 0`.

- [ ] **Step 5: Review branch scope**

```powershell
git status --short
git diff --check
git log --oneline origin/develop..HEAD
```

Expected: a clean worktree, no patch errors, and only intentional feature/spec/changelog commits beyond `origin/develop`.

- [ ] **Step 6: Push the current branch and verify PR #11**

```powershell
git push
gh pr view 11 --json url,state,baseRefName,headRefName,title
```

Expected: push succeeds; PR #11 remains `OPEN`, base `develop`, head `feat/updating-noxy-version-skill`, and no second PR is created.
