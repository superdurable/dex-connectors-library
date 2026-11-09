# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Guard the transition from legacy packages to required provider scenarios."""

import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from script import connector_conformance



class LegacyConformanceSelectionTest(unittest.TestCase):
    def test_go_only_change_warns_for_existing_package(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)), patch.object(connector_conformance, "conformance_exemptions", return_value={}):
            connector = Path(directory) / "connectors/legacy"
            (connector / "python").mkdir(parents=True)
            (connector / "connector.yaml").write_text("metadata: legacy\n")
            (connector / "python/pyproject.toml").write_text("[project]\n")
            warnings = io.StringIO()
            with contextlib.redirect_stderr(warnings):
                connector_conformance.run_connector(connector, ["connectors/legacy/go/client.go"])
            self.assertIn("missing conformance (legacy)", warnings.getvalue())

    def test_language_change_requires_fixture(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)), patch.object(connector_conformance, "conformance_exemptions", return_value={}):
            connector = Path(directory) / "connectors/legacy"
            (connector / "python").mkdir(parents=True)
            (connector / "connector.yaml").write_text("metadata: legacy\n")
            (connector / "python/pyproject.toml").write_text("[project]\n")
            with self.assertRaisesRegex(ValueError, "has no conformance scenarios"):
                connector_conformance.run_connector(connector, ["connectors/legacy/python/client.py"])


class NativeWireExemptionTest(unittest.TestCase):
    def test_reviewed_native_connectors_are_exempt_without_fixtures(self) -> None:
        exemptions = connector_conformance.conformance_exemptions()
        listed = json.loads((connector_conformance.ROOT / "conformance/exemptions.yaml").read_text())["exemptions"]
        self.assertEqual({entry["connector"] for entry in listed}, connector_conformance.NATIVE_WIRE_CONNECTORS)
        self.assertEqual(set(exemptions), {
            connector for connector in connector_conformance.NATIVE_WIRE_CONNECTORS
            if (connector_conformance.ROOT / connector / "connector.yaml").is_file()})
        for connector_path, reason in exemptions.items():
            with self.subTest(connector=connector_path), patch.object(connector_conformance.subprocess, "run") as run:
                output = io.StringIO()
                with contextlib.redirect_stdout(output):
                    connector_conformance.run_connector(
                        connector_conformance.ROOT / connector_path,
                        [connector_path + "/python/client.py"], exemptions)
                self.assertIn(f"exempt ({reason})", output.getvalue())
                run.assert_not_called()

    def test_http_connector_cannot_be_exempt(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)):
            root = Path(directory)
            (root / "connectors/stripe").mkdir(parents=True)
            (root / "connectors/stripe/connector.yaml").write_text("metadata: stripe\n")
            exemption_file = root / "conformance/exemptions.yaml"
            exemption_file.parent.mkdir()
            exemption_file.write_text(json.dumps({"schemaVersion": 1, "exemptions": [
                {"connector": "connectors/stripe", "reason": "convenient"}]}))
            with self.assertRaisesRegex(ValueError, "not an approved native-wire connector"):
                connector_conformance.conformance_exemptions()

    def test_exemption_of_absent_connector_is_skipped_until_it_lands(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)):
            root = Path(directory)
            (root / "connectors/mysql/python").mkdir(parents=True)
            (root / "connectors/mysql/connector.yaml").write_text("metadata: mysql\n")
            (root / "connectors/mysql/python/pyproject.toml").write_text("[project]\n")
            exemption_file = root / "conformance/exemptions.yaml"
            exemption_file.parent.mkdir()
            exemption_file.write_text(json.dumps({"schemaVersion": 1, "exemptions": [
                {"connector": "connectors/mysql", "reason": "wire"},
                {"connector": "connectors/mongodb", "reason": "wire"}]}))
            self.assertEqual({"connectors/mysql": "wire"}, connector_conformance.conformance_exemptions())
            self.assertEqual(["connectors/mysql"],
                             [entry["connector"] for entry in connector_conformance.database_ci_matrix([])])

    def test_present_directory_without_manifest_cannot_be_exempt(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)):
            root = Path(directory)
            (root / "connectors/mongodb").mkdir(parents=True)
            exemption_file = root / "conformance/exemptions.yaml"
            exemption_file.parent.mkdir()
            exemption_file.write_text(json.dumps({"schemaVersion": 1, "exemptions": [
                {"connector": "connectors/mongodb", "reason": "wire"}]}))
            with self.assertRaisesRegex(ValueError, "is not a connector"):
                connector_conformance.conformance_exemptions()

    def test_absent_duplicate_exemption_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)):
            exemption_file = Path(directory) / "conformance/exemptions.yaml"
            exemption_file.parent.mkdir()
            exemption_file.write_text(json.dumps({"schemaVersion": 1, "exemptions": [
                {"connector": "connectors/mongodb", "reason": "wire"}] * 2}))
            with self.assertRaisesRegex(ValueError, "repeats"):
                connector_conformance.conformance_exemptions()

    def test_duplicate_and_empty_reasons_are_rejected(self) -> None:
        for entries, error in [
            ([{"connector": "connectors/mysql", "reason": ""}], "no exemption reason"),
            ([{"connector": "connectors/mysql", "reason": "wire"}] * 2, "repeats"),
        ]:
            with self.subTest(error=error), tempfile.TemporaryDirectory() as directory, patch.object(
                connector_conformance, "ROOT", Path(directory)
            ):
                root = Path(directory)
                (root / "connectors/mysql").mkdir(parents=True)
                (root / "connectors/mysql/connector.yaml").write_text("metadata: mysql\n")
                exemption_file = root / "conformance/exemptions.yaml"
                exemption_file.parent.mkdir()
                exemption_file.write_text(json.dumps({"schemaVersion": 1, "exemptions": entries}))
                with self.assertRaisesRegex(ValueError, error):
                    connector_conformance.conformance_exemptions()

    def test_ci_matrix_selects_only_changed_exempt_packages(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)), patch.object(
            connector_conformance, "conformance_exemptions", return_value={
                "connectors/postgresql": "wire", "connectors/mysql": "wire"}
        ):
            root = Path(directory)
            postgres = root / "connectors/postgresql/python"
            mysql = root / "connectors/mysql/typescript"
            postgres.mkdir(parents=True)
            mysql.mkdir(parents=True)
            (postgres / "pyproject.toml").write_text("[project]\n")
            (mysql / "package.json").write_text("{}")
            matrix = connector_conformance.database_ci_matrix(["connectors/postgresql/python/src/client.py"])
            self.assertEqual(matrix, [{"connector": "connectors/postgresql", "image": "postgres:17", "port": "5432"}])
            shared = connector_conformance.database_ci_matrix(["conformance/exemptions.yaml"])
            self.assertEqual({item["connector"] for item in shared},
                             {"connectors/postgresql", "connectors/mysql"})

    def test_database_ci_matrix_ignores_email_package(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)), patch.object(
            connector_conformance, "conformance_exemptions", return_value={"connectors/superdurable/email": "wire"}
        ):
            package = Path(directory) / "connectors/superdurable/email/python"
            package.mkdir(parents=True)
            (package / "pyproject.toml").write_text("[project]\n")
            self.assertEqual(connector_conformance.database_ci_matrix(["conformance/exemptions.yaml"]), [])

    def test_email_ci_matrix_selects_only_changed_exempt_packages(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)), patch.object(
            connector_conformance, "conformance_exemptions", return_value={"connectors/superdurable/email": "wire"}
        ):
            root = Path(directory)
            package = root / "connectors/superdurable/email/python"
            package.mkdir(parents=True)
            (package / "pyproject.toml").write_text("[project]\n")
            self.assertEqual(connector_conformance.email_ci_matrix(["connectors/mysql/go/client.go"]), [])
            expected = [{"connector": "connectors/superdurable/email"}]
            self.assertEqual(connector_conformance.email_ci_matrix(["connectors/superdurable/email/python/src/client.py"]), expected)
            self.assertEqual(connector_conformance.email_ci_matrix(["conformance/exemptions.yaml"]), expected)
            (package / "pyproject.toml").unlink()
            self.assertEqual(connector_conformance.email_ci_matrix(["conformance/exemptions.yaml"]), [])


class CoverageValidationTest(unittest.TestCase):
    def test_optional_clock_requires_rfc3339_timestamp(self) -> None:
        cases = json.loads((Path(__file__).resolve().parents[1] / "conformance/runners/clock_cases.json").read_text())
        for clock in cases["valid"]:
            with self.subTest(clock=clock):
                connector_conformance.validate_scenario_fixed_clock({"name": "fixed", "fixedClock": clock})
        for clock in cases["invalid"]:
            with self.subTest(clock=clock):
                with self.assertRaises(ValueError):
                    connector_conformance.validate_scenario_fixed_clock({"name": "fixed", "fixedClock": clock})

    def validate_fixture(self, scenarios: list[dict[str, object]], error: str) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)), patch.object(connector_conformance, "conformance_exemptions", return_value={}):
            root = Path(directory)
            connector = root / "connectors/example"
            connector.mkdir(parents=True)
            (connector / "connector.yaml").write_text("metadata: example\n")
            fixture = root / "conformance/connectors/example/scenarios.json"
            fixture.parent.mkdir(parents=True)
            fixture.write_text(json.dumps({"schemaVersion": 1, "connector": "example", "scenarios": scenarios}))
            operations = json.dumps({"readItem": {"kind": "query", "branches": ["read"],
                                                   "optionalInputFields": []}})
            with patch.object(connector_conformance.subprocess, "check_output", return_value=operations):
                with self.assertRaisesRegex(ValueError, error):
                    connector_conformance.run_connector(connector, [])

    @staticmethod
    def scenario(name: str, statuses: list[int | None], retry: bool) -> dict[str, object]:
        exchanges = [{"transportFailure": "before-send"} if status is None else {"status": status}
                     for status in statuses]
        expected = {"branch": "" if retry else "read", "failureKind": "", "retry": retry,
                    "retryAfterMs": 0, "uncertain": False,
                    "requestCount": sum(status is not None for status in statuses)}
        if not retry:
            expected["output"] = {}
        return {"name": name, "operation": "readItem", "callId": name, "input": {},
                "exchanges": exchanges, "expected": expected}

    def test_5xx_must_be_first_exchange_of_final_attempt(self) -> None:
        scenarios = [self.scenario("rate", [429], True), self.scenario("server", [200, 503], True),
                     self.scenario("transport", [None], True), self.scenario("read", [200], False)]
        self.validate_fixture(scenarios, "separate 429 and 5xx")

    def test_earlier_attempt_5xx_does_not_count(self) -> None:
        server = self.scenario("server", [503, 200], True)
        server["attempts"] = 2
        server["intermediate"] = [{"failureKind": "AVAILABILITY", "retryAfterMs": 0,
                                   "exchangeCount": 1}]
        scenarios = [self.scenario("rate", [429], True), server,
                     self.scenario("transport", [None], True), self.scenario("read", [200], False)]
        self.validate_fixture(scenarios, "separate 429 and 5xx")

    def test_transport_failure_required_for_read(self) -> None:
        scenarios = [self.scenario("rate", [429], True), self.scenario("server", [503], True),
                     self.scenario("read", [200], False)]
        self.validate_fixture(scenarios, "transport failure coverage")

    def test_pending_is_rejected(self) -> None:
        scenario = self.scenario("read", [200], False)
        scenario["pending"] = True
        self.validate_fixture([scenario], "unsupported pending")


if __name__ == "__main__":
    unittest.main()
