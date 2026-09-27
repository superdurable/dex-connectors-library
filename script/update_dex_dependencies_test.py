#!/usr/bin/env python3
# SPDX-License-Identifier: MIT

import tempfile
import unittest
from pathlib import Path

from script.update_dex_dependencies import update


OLD_CHECKSUM = "1" * 64
NEW_CHECKSUM = "2" * 64


class UpdateDexDependenciesTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        (self.root / "sdkgo").mkdir()
        (self.root / "connectors" / "demo").mkdir(parents=True)
        (self.root / "sdkgo" / "go.mod").write_text(
            "module example.com/sdkgo\n\n"
            "require (\n"
            "\tgithub.com/superdurable/dex/blob-cache-go v0.1.0\n"
            "\tgithub.com/superdurable/dex/sdk-go v0.11.3\n"
            ")\n"
        )
        (self.root / "connectors" / "demo" / "go.mod").write_text(
            "module example.com/demo\n\n"
            "require github.com/superdurable/dex/sdk-go v0.11.3\n\n"
            "require github.com/superdurable/dex/blob-cache-go v0.1.0 // indirect\n"
        )
        (self.root / ".dex-compat-version").write_text("cli-v0.11.3\n")
        (self.root / "DEX_CLI_LINUX_AMD64_SHA256").write_text(OLD_CHECKSUM + "\n")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def test_updates_every_external_dex_pin_and_is_idempotent(self) -> None:
        result = update(
            self.root, "v0.13.1", "v0.2.0", "cli-v0.13.8", NEW_CHECKSUM
        )
        self.assertEqual(result["changed"], "true")
        for path in (
            self.root / "sdkgo" / "go.mod",
            self.root / "connectors" / "demo" / "go.mod",
        ):
            content = path.read_text()
            self.assertIn("github.com/superdurable/dex/sdk-go v0.13.1", content)
            self.assertIn("github.com/superdurable/dex/blob-cache-go v0.2.0", content)
        self.assertEqual(
            (self.root / ".dex-compat-version").read_text(), "cli-v0.13.8\n"
        )
        self.assertEqual(
            (self.root / "DEX_CLI_LINUX_AMD64_SHA256").read_text(),
            NEW_CHECKSUM + "\n",
        )

        second = update(
            self.root, "v0.13.1", "v0.2.0", "cli-v0.13.8", NEW_CHECKSUM
        )
        self.assertEqual(second["changed"], "false")

    def test_rejects_changed_checksum_for_the_same_release(self) -> None:
        with self.assertRaisesRegex(SystemExit, "checksum changed"):
            update(
                self.root,
                "v0.11.3",
                "v0.1.0",
                "cli-v0.11.3",
                NEW_CHECKSUM,
            )

    def test_rejects_unsynchronized_module_versions(self) -> None:
        connector_go_mod = self.root / "connectors" / "demo" / "go.mod"
        connector_go_mod.write_text(
            connector_go_mod.read_text().replace("v0.11.3", "v0.11.2")
        )
        with self.assertRaisesRegex(SystemExit, "pins are not synchronized"):
            update(
                self.root,
                "v0.13.1",
                "v0.1.0",
                "cli-v0.11.3",
                OLD_CHECKSUM,
            )

    def test_rejects_dependency_downgrade(self) -> None:
        with self.assertRaisesRegex(SystemExit, "is older than current"):
            update(
                self.root,
                "v0.10.0",
                "v0.1.0",
                "cli-v0.11.3",
                OLD_CHECKSUM,
            )


if __name__ == "__main__":
    unittest.main()
