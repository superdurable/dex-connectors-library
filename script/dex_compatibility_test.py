# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

import re
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from script.dex_compatibility import (
    connector_releases,
    create_module_proxy,
    example_consumer_name,
    flow_examples,
    manifests,
    parse_connector_directories,
)


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


class ConnectorSelectionTest(unittest.TestCase):
    def test_selected_manifests_keep_requested_order(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for directory in ("connectors/acme/chat", "connectors/acme/mail"):
                connector = root / directory
                connector.mkdir(parents=True)
                (connector / "connector.yaml").write_text("manifest")

            selected = parse_connector_directories(root, "connectors/acme/mail,connectors/acme/chat")

            self.assertEqual(
                [root / "connectors/acme/mail/connector.yaml", root / "connectors/acme/chat/connector.yaml"],
                manifests(root, selected),
            )

    def test_empty_selection_has_no_manifests(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)

            self.assertEqual([], manifests(root, parse_connector_directories(root, "")))

    def test_flow_examples_only_include_selected_connectors(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for directory in ("connectors/acme/chat", "connectors/acme/mail"):
                flow = root / directory / "examples" / "example" / "flow"
                flow.mkdir(parents=True)
                (flow / "flow.go").write_text("package flow\n\nfunc GetSteps() {}\n")

            self.assertEqual(
                [root / "connectors/acme/mail/examples/example/flow"],
                flow_examples(root, ["connectors/acme/mail"]),
            )
            self.assertEqual([], flow_examples(root, []))

    def test_released_compatibility_only_includes_selected_connectors(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for directory in ("connectors/acme/chat", "connectors/acme/mail"):
                connector = root / directory
                connector.mkdir(parents=True)
                (connector / "connector.yaml").write_text("manifest")
            releases = [
                {"tag_name": "connectors/acme/chat/v0.1.0", "draft": False, "prerelease": False},
                {"tag_name": "connectors/acme/mail/v0.1.0", "draft": False, "prerelease": False},
            ]

            with patch("script.dex_compatibility.github_releases", return_value=releases):
                selected = connector_releases(root, None, ["connectors/acme/mail"])

            self.assertEqual(["connectors/acme/mail/v0.1.0"], [release["tag_name"] for release in selected])


if __name__ == "__main__":
    unittest.main()
