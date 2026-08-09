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
