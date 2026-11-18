# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the Python conformance clock and fixture decoder."""

import asyncio
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from connector_scenarios import HeartbeatContext, _same_heartbeat, load_scenarios
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


class HeartbeatEncodingTest(unittest.TestCase):
    def test_records_marker_and_checkpoint_map_heartbeats(self) -> None:
        call_id = "5af8fafa-06ff-5b2d-bb13-04693809fabf"
        marker = {"callId": call_id, "attempt": 1, "state": "claimed"}
        checkpoint = {"singleDispatchMarkers": {call_id: marker}}
        context = HeartbeatContext()
        asyncio.run(context.heartbeat(checkpoint))
        self.assertEqual(context.heartbeat_payload, checkpoint)
        self.assertTrue(context.has_heartbeat_value)
        asyncio.run(context.heartbeat(Marker.from_wire(marker)))
        self.assertEqual(context.heartbeat_payload, marker)


class HeartbeatFormatTranslationTest(unittest.TestCase):
    call_id = "5af8fafa-06ff-5b2d-bb13-04693809fabf"
    marker = {"callId": call_id, "attempt": 1, "state": "claimed", "sendCount": 1}
    checkpoint = {"singleDispatchMarkers": {call_id: marker}}

    def test_reads_a_flat_recording_as_the_checkpoint_map(self) -> None:
        context = HeartbeatContext({**self.marker}, call_id=self.call_id)
        self.assertEqual(context.get_last_heartbeat_value(dict[str, Any]), self.checkpoint)
        self.assertEqual(context.get_last_heartbeat_value(Marker), Marker.from_wire(self.marker))

    def test_reads_a_map_recording_as_its_own_marker(self) -> None:
        context = HeartbeatContext({"attempt": 1, "value": self.checkpoint}, call_id=self.call_id)
        self.assertEqual(context.get_last_heartbeat_value(Marker), Marker.from_wire(self.marker))
        self.assertEqual(context.get_last_heartbeat_value(dict[str, Any]), self.checkpoint)

    def test_keeps_foreign_and_multi_entry_heartbeats_unchanged(self) -> None:
        foreign = {**self.marker, "callId": "b8b7c0e5-3d8c-5d5e-9a51-0c4d2b7e6f10"}
        context = HeartbeatContext({"attempt": 1, "value": foreign}, call_id=self.call_id)
        self.assertEqual(context.get_last_heartbeat_value(dict[str, Any]), foreign)
        both = {"singleDispatchMarkers": {self.call_id: self.marker, foreign["callId"]: foreign}}
        self.assertFalse(_same_heartbeat(both, self.marker, self.call_id))

    def test_compares_either_format_exactly(self) -> None:
        self.assertTrue(_same_heartbeat(self.checkpoint, self.marker, self.call_id))
        self.assertTrue(_same_heartbeat(self.marker, self.checkpoint, self.call_id))
        self.assertFalse(_same_heartbeat(self.checkpoint, {**self.marker, "sendCount": 2}, self.call_id))
        self.assertFalse(_same_heartbeat({"__wrong_heartbeat_shape__": True}, self.marker, self.call_id))


if __name__ == "__main__":
    unittest.main()
