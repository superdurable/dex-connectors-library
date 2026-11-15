# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the Python conformance clock and fixture decoder."""

import json
import tempfile
import unittest
from pathlib import Path

from connector_scenarios import load_scenarios
from dex_connectors.sdk.singledispatch import Marker


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


class PriorHeartbeatTest(unittest.TestCase):
    def test_marker_shorthand_send_count(self) -> None:
        source = json.loads((ROOT / "conformance/connectors/airtable/scenarios.json").read_text())
        scenario = source["scenarios"][0]
        # SDK releases before 0.10.0 decode no sendCount, so their marker rejects every count.
        does_marker_accept_send_count = "send_count" in getattr(Marker, "__dataclass_fields__", {})
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "scenarios.json"
            for send_count in (0, 2):
                with self.subTest(send_count=send_count):
                    scenario["priorHeartbeat"] = {"callId": scenario["callId"], "attempt": 2, "state": "claimed",
                                                  "sendCount": send_count}
                    path.write_text(json.dumps(source))
                    if does_marker_accept_send_count:
                        self.assertEqual(load_scenarios(path)[0]["priorHeartbeat"]["sendCount"], send_count)
                    else:
                        self.assertRaises(ValueError, load_scenarios, path)
            for send_count in (-1, 1.5, "2", None, True):
                with self.subTest(send_count=send_count):
                    scenario["priorHeartbeat"] = {"callId": scenario["callId"], "attempt": 2, "state": "claimed",
                                                  "sendCount": send_count}
                    path.write_text(json.dumps(source))
                    self.assertRaises(ValueError, load_scenarios, path)


if __name__ == "__main__":
    unittest.main()
