# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Segment provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.twilio_segment as segment

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/twilio/segment/scenarios.json")
CONNECTION = sdk.ConnectionRef("segment", "segment")
TEST_CREDENTIALS = segment.Credentials(sdk.SecretString("sample-segment-api-key"))
ADAPTER = conformance.PythonOperationAdapter(
    "segment",
    CONNECTION,
    segment,
    lambda http_client, _recorder: segment.Client(
        segment.Config(base_url="http://127.0.0.1:8848/api"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/api")
