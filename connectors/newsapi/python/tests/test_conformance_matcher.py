# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Apply one matcher fixture to the Python provider recorder."""

import json
import sys
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from types import SimpleNamespace

import httpx
import pytest

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import ExchangeRecorder, SyntheticRandomSource, assert_attempt  # noqa: E402

CASES = json.loads((ROOT / "conformance/runners/matcher_cases.json").read_text())


def test_synthetic_random_stream_matches_go() -> None:
    """Match Go byte blocks and rejection sampling across block boundaries."""
    seed = "SYNTHETIC_CONFORMANCE_PARITY_VECTOR"
    assert SyntheticRandomSource(seed).read(64).hex() == (
        "8340adfda3332f0d504929aa79cc566bfd436f83db3b60780dc6dd4c666b777d"
        "891789301f0e1cca5155d82b3cdea5a89baeb0cfc50cac2c00eb26ca6a40565f"
    )
    source = SyntheticRandomSource(seed)
    assert [source.go_random_int(maximum) for maximum in (26, 26, 10, 12, 74, 44, 43, 1, 256, 257)] == (
        [3, 0, 3, 3, 47, 13, 16, 0, 73, 107]
    )


@dataclass
class CodecOutput:
    """Exercise the actual Dex SDK timestamp and default metadata."""

    received_at: datetime = field(metadata={"dex_json_name": "receivedAt", "dex_json_format": "rfc3339"})
    label: str = field(default="fallback", metadata={"dex_json_omit_default": True})


@dataclass
class WrongCodecOutput:
    """Simulate a porter dropping the SDK's RFC3339 annotation."""

    received_at: datetime = field(metadata={"dex_json_name": "receivedAt"})
    label: str = field(default="fallback", metadata={"dex_json_omit_default": True})


def test_output_uses_sdk_codec_metadata() -> None:
    """Reject a missing RFC3339 annotation and honor the declared default."""
    expected = {"branch": "read", "failureKind": "", "retry": False, "uncertain": False,
                "retryAfterMs": 0, "output": {"receivedAt": "2026-01-02T00:00:00Z", "label": ""}}
    timestamp = datetime(2026, 1, 2, tzinfo=UTC)
    attempt = SimpleNamespace(kind="branch", branch="read", failure=None, retry_after_seconds=0,
                              value=CodecOutput(timestamp, ""))
    assert_attempt(attempt, expected)
    attempt.value = WrongCodecOutput(timestamp, "")
    with pytest.raises(AssertionError, match="output"):
        assert_attempt(attempt, expected)


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
async def test_conformance_matcher(case: dict[str, object]) -> None:
    """Distinguish missing, empty, repeated, array, and decoded MIME fields."""
    scenario = {"callId": "call", "exchanges": [case["exchange"]], "expected": {"requestCount": 1}}
    recorder = ExchangeRecorder(scenario, "call")
    headers = [(name, value) for name, values in case["requestHeaders"].items() for value in values]
    request = httpx.Request(case["exchange"]["method"], "http://localhost" + case["requestPath"],
                            headers=headers, content=case["requestBody"])
    await recorder.answer(request)
    assert len(recorder.mismatches) == len(case["expectedMismatches"]), recorder.mismatches
    for expected_field in case["expectedMismatches"]:
        assert any(expected_field in mismatch for mismatch in recorder.mismatches)
