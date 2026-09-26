# Copyright (c) 2026 Super Durable, Inc.
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import re
import tempfile
import unittest
from pathlib import Path

from script.dex_compatibility import create_module_proxy


class ModuleProxyTest(unittest.TestCase):
    def test_synthetic_version_changes_with_module_contents(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            module = root / "connector"
            module.mkdir()
            (module / "go.mod").write_text("module example.com/connector\n\ngo 1.24\n")
            source = module / "connector.go"
            source.write_text("package connector\n\nconst Version = 1\n")

            _, first = create_module_proxy(module, root / "proxy-one")
            source.write_text("package connector\n\nconst Version = 2\n")
            _, second = create_module_proxy(module, root / "proxy-two")

            self.assertRegex(first, re.compile(r"^v0\.0\.[1-9][0-9]*$"))
            self.assertNotEqual(first, second)


if __name__ == "__main__":
    unittest.main()
