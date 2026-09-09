#!/usr/bin/env python3
"""Regression tests for migration generation and repository validation."""

from concurrent.futures import ProcessPoolExecutor
from pathlib import Path
import tempfile
import unittest

from manage import create, validate


class MigrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.directory = self.root / "post"
        self.directory.mkdir()
        self.pair(1)

    def pair(self, version):
        for direction in ("up", "down"):
            (self.directory / f"{version:06d}_example.{direction}.sql").write_text("SELECT 1;\n")

    def test_next_version_and_content_required_before_check(self):
        paths = create(self.root, "post", "add_example")
        self.assertEqual(paths[0].name, "000002_add_example.up.sql")
        with self.assertRaisesRegex(ValueError, "no SQL"):
            validate(self.directory)
        for path in paths:
            path.write_text("SELECT 2;\n")
        self.assertEqual(validate(self.directory), 2)

    def test_invalid_name_and_retired_module_do_not_create_files(self):
        for name in ("../escape", "add-name", "UPPER", "$(touch injected)", "", "_field"):
            with self.subTest(name=name), self.assertRaises(ValueError):
                create(self.root, "post", name)
        with self.assertRaises(ValueError):
            create(self.root, "bot", "add_field")
        self.assertEqual(len(list(self.directory.iterdir())), 2)

    def test_gap_rejected(self):
        self.pair(3)
        with self.assertRaisesRegex(ValueError, "gaps"):
            create(self.root, "post", "next")

    def test_duplicate_version_rejected(self):
        (self.directory / "000001_duplicate.up.sql").write_text("SELECT 1;")
        with self.assertRaisesRegex(ValueError, "duplicate"):
            validate(self.directory)

    def test_missing_down_rejected(self):
        (self.directory / "000001_example.down.sql").unlink()
        with self.assertRaisesRegex(ValueError, "pair"):
            create(self.root, "post", "next")

    def test_mismatched_names_rejected(self):
        (self.directory / "000001_example.down.sql").rename(self.directory / "000001_wrong.down.sql")
        with self.assertRaisesRegex(ValueError, "pair"):
            validate(self.directory)

    def test_noncanonical_filename_rejected(self):
        (self.directory / "2_example.up.sql").write_text("SELECT 1;")
        with self.assertRaisesRegex(ValueError, "filename"):
            validate(self.directory)

    def test_parallel_creates_use_distinct_versions(self):
        with ProcessPoolExecutor(max_workers=4) as executor:
            results = [executor.submit(create, self.root, "post", f"change_{i}") for i in range(4)]
            versions = {result.result()[0].name[:6] for result in results}
        self.assertEqual(versions, {"000002", "000003", "000004", "000005"})
        self.assertEqual(validate(self.directory, allow_empty=True), 5)


if __name__ == "__main__":
    unittest.main()
