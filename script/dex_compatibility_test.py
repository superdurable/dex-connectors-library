# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

import re
import tempfile
import unittest
from pathlib import Path

from script.dex_compatibility import create_module_proxy, example_consumer_name


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


class ExampleConsumerNameTest(unittest.TestCase):
    def test_same_example_name_in_two_connectors_gets_distinct_consumers(self) -> None:
        meta = example_consumer_name("connectors/meta", Path("connectors/meta/examples/summarize-text/flow"))
        claude = example_consumer_name("connectors/anthropic", Path("connectors/anthropic/examples/summarize-text/flow"))
        self.assertEqual("meta-summarize-text", meta)
        self.assertEqual("anthropic-summarize-text", claude)

    def test_nested_company_directories_are_part_of_the_name(self) -> None:
        name = example_consumer_name("connectors/google/gemini", Path("x/examples/generate-summary/flow"))
        self.assertEqual("google-gemini-generate-summary", name)


if __name__ == "__main__":
    unittest.main()
