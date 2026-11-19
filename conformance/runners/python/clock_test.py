# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the Python conformance clock and fixture decoder."""

import json
import tempfile
import unittest
from pathlib import Path
import httpx

from connector_scenarios import ExchangeRecorder, load_scenarios
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


class SignedFormTest(unittest.IsolatedAsyncioTestCase):
    def test_signed_form_fixture_validation(self) -> None:
        source = json.loads((ROOT / "conformance/connectors/airtable/scenarios.json").read_text())
        scenario = source["scenarios"][0]
        scenario["randomSeed"] = "SYNTHETIC_CONFORMANCE_FORM"
        scenario["randomFormFields"] = ["salt"]
        scenario["exchanges"][0]["form"] = {"salt": {"$syntheticSha256": "a" * 64}}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "scenarios.json"
            path.write_text(json.dumps(source))
            self.assertEqual(load_scenarios(path)[0]["randomFormFields"], ["salt"])
            scenario["exchanges"][0]["form"]["salt"]["$syntheticSha256"] = "invalid"
            path.write_text(json.dumps(source))
            self.assertRaises(ValueError, load_scenarios, path)

    async def test_youdao_shaped_signed_form(self) -> None:
        cases = json.loads((ROOT / "conformance/runners/matcher_cases.json").read_text())
        for case in cases:
            if not case["name"].startswith("signed-form-"):
                continue
            with self.subTest(case=case["name"]):
                recorder = ExchangeRecorder({"exchanges": [case["exchange"]]}, "call")
                request = httpx.Request("POST", "http://localhost" + case["requestPath"],
                                        headers={name: values[0] for name, values in case["requestHeaders"].items()},
                                        content=case["requestBody"])
                await recorder.answer(request)
                self.assertEqual(recorder.request_count, 1)
                self.assertEqual(len(recorder.mismatches), len(case["expectedMismatches"]))
                for expected in case["expectedMismatches"]:
                    self.assertIn(expected, "\n".join(recorder.mismatches))


if __name__ == "__main__":
    unittest.main()
