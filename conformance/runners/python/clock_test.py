# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the Python conformance clock and fixture decoder."""

import json
import tempfile
import unittest
from pathlib import Path

from connector_scenarios import load_scenarios


ROOT = Path(__file__).resolve().parents[3]


class ClockTest(unittest.TestCase):
    def test_shared_clock_cases(self) -> None:
        cases = json.loads((ROOT / "conformance/runners/clock_cases.json").read_text())
        source = json.loads((ROOT / "conformance/connectors/airtable/scenarios.json").read_text())
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "scenarios.json"
            for clock in cases["valid"]:
                with self.subTest(clock=clock):
                    source["scenarios"][0]["fixedClock"] = clock
                    path.write_text(json.dumps(source))
                    self.assertEqual(load_scenarios(path)[0]["fixedClock"], clock)
            for clock in cases["invalid"]:
                with self.subTest(clock=clock), self.assertRaises((ValueError, TypeError)):
                    source["scenarios"][0]["fixedClock"] = clock
                    path.write_text(json.dumps(source))
                    load_scenarios(path)


if __name__ == "__main__":
    unittest.main()
