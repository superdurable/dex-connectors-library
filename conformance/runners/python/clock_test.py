# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the Python conformance clock and fixture decoder."""

import json
import tempfile
import unittest
from pathlib import Path

from connector_scenarios import load_scenarios, scenario_clock


ROOT = Path(__file__).resolve().parents[3]


class ClockTest(unittest.TestCase):
    def test_shared_clock_cases(self) -> None:
        cases = json.loads((ROOT / "conformance/runners/clock_cases.json").read_text())
        for clock in cases["valid"]:
            with self.subTest(clock=clock):
                self.assertEqual(scenario_clock({"name": "fixed", "clock": clock})().isoformat()[:19], clock[:19])
        for clock in cases["invalid"]:
            with self.subTest(clock=clock), self.assertRaises(ValueError):
                scenario_clock({"name": "fixed", "clock": clock})

    def test_loads_clocked_fixture(self) -> None:
        document = json.loads((ROOT / "conformance/connectors/airtable/scenarios.json").read_text())
        document["scenarios"][0]["clock"] = "2026-10-26T12:00:00.120Z"
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "scenarios.json"
            path.write_text(json.dumps(document))
            self.assertEqual(load_scenarios(path)[0]["clock"], document["scenarios"][0]["clock"])


if __name__ == "__main__":
    unittest.main()
