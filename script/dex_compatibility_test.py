# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

import re
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from script.dex_compatibility import (
    CONNECTION_RECORD_FIXTURES,
    CREDENTIAL_ISOLATION_FIXTURE,
    CREDENTIAL_ISOLATION_MODULE_PATH,
    connector_releases,
    create_module_proxy,
    dex_web_compatibility_sources,
    dex_web_compatibility_test_names,
    example_consumer_name,
    fixture_module_path,
    flow_examples,
    manifests,
    parse_connector_directories,
    require_dex_web_security_tests_passed,
    require_dex_web_tests_passed,
)

ROOT = Path(__file__).resolve().parents[1]


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


class DexWebCompatibilityTestSelectionTest(unittest.TestCase):
    def test_every_repository_compatibility_test_is_selected(self) -> None:
        names = dex_web_compatibility_test_names(dex_web_compatibility_sources(ROOT))
        self.assertIn("TestExternalConnectorCompatibility", names)
        self.assertIn("TestStudioCommandCredentialIsolation", names)
        self.assertIn("TestConnectorConnectionRecordContract", names)

    def test_only_top_level_test_functions_are_selected(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary) / "fixture_test.go.txt"
            source.write_text(
                "package web\n\n"
                "func TestSelected(t *testing.T) {\n}\n\n"
                "func TestRenamedParameter(test *testing.T) {\n}\n\n"
                "func TestTrailingComment(t *testing.T) { // scenario\n}\n\n"
                "func (scenario *fixtureScenario) TestNotTopLevel(t *testing.T) {\n}\n\n"
                "func testHelper(t *testing.T) {\n}\n\n"
                "func TestMain(m *testing.M) {\n}\n"
            )
            self.assertEqual(
                ["TestSelected", "TestRenamedParameter", "TestTrailingComment"],
                dex_web_compatibility_test_names([source]),
            )

    def test_source_without_top_level_tests_fails(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            selected = Path(temporary) / "selected_test.go.txt"
            selected.write_text("package web\n\nfunc TestSelected(t *testing.T) {\n}\n")
            unselected = Path(temporary) / "unselected_test.go.txt"
            unselected.write_text("package web\n\nfunc testHelper(t *testing.T) {\n}\n")
            with self.assertRaisesRegex(RuntimeError, "unselected_test.go.txt"):
                dex_web_compatibility_test_names([selected, unselected])

    def test_passing_run_is_accepted(self) -> None:
        output = "=== RUN   TestSelected\n--- PASS: TestSelected (0.02s)\nPASS\n"
        require_dex_web_tests_passed(output, ["TestSelected"])

    def test_unselected_test_fails_even_when_go_test_passes(self) -> None:
        output = "testing: warning: no tests to run\nPASS\nok  \tgithub.com/superdurable/dex/web\t0.1s\n"
        with self.assertRaisesRegex(RuntimeError, "TestSelected"):
            require_dex_web_tests_passed(output, ["TestSelected"])

    def test_passing_subtest_does_not_stand_in_for_its_parent(self) -> None:
        output = "    --- PASS: TestSelected/phase (0.01s)\n--- FAIL: TestSelected (0.02s)\n"
        with self.assertRaisesRegex(RuntimeError, "TestSelected"):
            require_dex_web_tests_passed(output, ["TestSelected"])

    def test_security_prefix_is_satisfied_by_a_passing_test_it_selects(self) -> None:
        output = "--- PASS: TestConnectorOAuthUsesPKCE (0.02s)\n--- PASS: TestSlackOAuth (0.01s)\n"
        require_dex_web_security_tests_passed(output, ("TestConnectorOAuth", "TestSlackOAuth"))

    def test_security_prefix_that_selects_nothing_fails(self) -> None:
        output = "--- PASS: TestConnectorOAuthUsesPKCE (0.02s)\n    --- PASS: TestSlackOAuthRenamed/phase (0.01s)\n"
        with self.assertRaisesRegex(RuntimeError, "TestSlackOAuth"):
            require_dex_web_security_tests_passed(output, ("TestConnectorOAuth", "TestSlackOAuth"))


class DexWebFixtureTest(unittest.TestCase):
    def test_credential_isolation_keeps_its_module_path(self) -> None:
        self.assertEqual(CREDENTIAL_ISOLATION_MODULE_PATH, fixture_module_path(CREDENTIAL_ISOLATION_FIXTURE))

    def test_connection_record_fixtures_cover_single_and_multiple_named_methods(self) -> None:
        selections = []
        for fixture in CONNECTION_RECORD_FIXTURES:
            manifest = (ROOT / fixture / "connector.yaml").read_text()
            self.assertIn("    methods:\n", manifest, fixture)
            selections.append("multiple" if "    selection: multiple\n" in manifest else "single")
        self.assertEqual(["single", "multiple"], selections)
        self.assertEqual(len(CONNECTION_RECORD_FIXTURES), len({fixture_module_path(fixture) for fixture in CONNECTION_RECORD_FIXTURES}))


if __name__ == "__main__":
    unittest.main()
