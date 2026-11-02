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
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)):
            connector = Path(directory) / "connectors/legacy"
            (connector / "python").mkdir(parents=True)
            (connector / "connector.yaml").write_text("metadata: legacy\n")
            (connector / "python/pyproject.toml").write_text("[project]\n")
            warnings = io.StringIO()
            with contextlib.redirect_stderr(warnings):
                connector_conformance.run_connector(connector, ["connectors/legacy/go/client.go"])
            self.assertIn("missing conformance (legacy)", warnings.getvalue())

    def test_language_change_requires_fixture(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)):
            connector = Path(directory) / "connectors/legacy"
            (connector / "python").mkdir(parents=True)
            (connector / "connector.yaml").write_text("metadata: legacy\n")
            (connector / "python/pyproject.toml").write_text("[project]\n")
            with self.assertRaisesRegex(ValueError, "has no conformance scenarios"):
                connector_conformance.run_connector(connector, ["connectors/legacy/python/client.py"])


class CoverageValidationTest(unittest.TestCase):
    def validate_fixture(self, scenarios: list[dict[str, object]], error: str) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.object(connector_conformance, "ROOT", Path(directory)):
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
