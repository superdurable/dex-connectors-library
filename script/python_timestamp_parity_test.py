# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go time.Time JSON cases through generated Python timestamp parsing."""

import json
import re
import types
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "cmd/connectorctl/python_timestamp.py.txt"
CASES = ROOT / "internal/codegen/testdata/go_time_json_cases.json"
timestamp_namespace: dict[str, object] = {}
exec(compile(SOURCE.read_text(), str(SOURCE), "exec"), timestamp_namespace)
module = types.SimpleNamespace(**timestamp_namespace)


class GoTimestampParityTest(unittest.TestCase):
    def test_go_unmarshal_and_marshal_cases(self) -> None:
        for case in json.loads(CASES.read_text()):
            value = case["input"]
            with self.subTest(value=value):
                try:
                    module._parts(value, "updated_since")
                    accepted = True
                except ValueError:
                    accepted = False
                self.assertEqual(accepted, case["accepted"])
                if not accepted:
                    continue
                if case.get("marshalError"):
                    self.assertEqual(module.canonical_go_timestamp(value, "updated_since"), value)
                    with self.assertRaisesRegex(ValueError, "updated_since"):
                        module.parse_go_datetime(value, "updated_since")
                    continue
                canonical = module.canonical_go_timestamp(value, "updated_since")
                self.assertEqual(canonical, case["output"])
                fraction = re.search(r"[.,](\d+)", value)
                nanoseconds = int((fraction.group(1) if fraction else "")[:9].ljust(9, "0"))
                representable = not value.startswith("0000-") and nanoseconds % 1000 == 0
                if representable:
                    parsed = module.parse_go_datetime(value, "updated_since")
                    self.assertEqual(parsed.isoformat()[:19], canonical[:19])
                else:
                    with self.assertRaisesRegex(ValueError, "updated_since"):
                        module.parse_go_datetime(value, "updated_since")


if __name__ == "__main__":
    unittest.main()
