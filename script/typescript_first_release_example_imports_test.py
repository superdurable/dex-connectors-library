# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Regression tests for unpublished TypeScript connector example imports."""

import tempfile
import unittest
from pathlib import Path

from script.typescript_first_release_example_imports import find_unpublished_package_imports


class FirstReleaseExampleImportsTest(unittest.TestCase):
    def test_self_import_fails_with_source_location(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            package_directory = Path(temporary_directory)
            example_directory = package_directory / "examples" / "lookup"
            example_directory.mkdir(parents=True)
            (example_directory / "flow.ts").write_text(
                'import { newLookupStep } from "@superdurable/connector-new";\n'
            )
            self.assertEqual(
                [f"{example_directory / 'flow.ts'}:1: imports unpublished @superdurable/connector-new"],
                find_unpublished_package_imports(package_directory, "@superdurable/connector-new"),
            )

    def test_local_source_and_other_packages_pass(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            package_directory = Path(temporary_directory)
            example_directory = package_directory / "examples"
            example_directory.mkdir()
            (example_directory / "flow.ts").write_text(
                'import { newLookupStep } from "../../src/index.js";\n'
                'import { Client } from "@superdurable/dex";\n'
            )
            self.assertEqual(
                [], find_unpublished_package_imports(package_directory, "@superdurable/connector-new")
            )

    def test_type_only_and_subpath_imports_fail(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            package_directory = Path(temporary_directory)
            example_directory = package_directory / "examples"
            example_directory.mkdir()
            (example_directory / "flow.ts").write_text(
                'import type { Connection } from\n  "@superdurable/connector-new";\n'
                'const mock = await import("@superdurable/connector-new/mock");\n'
            )
            self.assertEqual(
                2, len(find_unpublished_package_imports(package_directory, "@superdurable/connector-new"))
            )


if __name__ == "__main__":
    unittest.main()
