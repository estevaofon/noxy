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

    def test_rejects_explicit_empty_release_date_without_writing(self) -> None:
        before = snapshot(self.root)

        with self.assertRaisesRegex(UpdateError, "release date"):
            execute_update(self.root, "1.6.0", "", False)

        self.assertEqual(snapshot(self.root), before)

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
