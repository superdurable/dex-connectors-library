# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Qiniu bucket scenarios through the Python Query."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.qiniu as qiniu

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/qiniu/scenarios.json")
CONNECTION = sdk.ConnectionRef("qiniu", "qiniu")
FIXED_TIME = datetime(2026, 11, 14, 0, 30, 5, tzinfo=UTC)
ADAPTER = conformance.PythonOperationAdapter(
    "qiniu",
    CONNECTION,
    qiniu,
    lambda http_client, recorder: qiniu.Client(
        qiniu.Config(
            base_url="http://127.0.0.1:12345",
            max_response_bytes=16 if recorder.scenario["name"] == "listBuckets-oversized" else 262144,
        ),
        conformance.static_credentials(
            CONNECTION,
            qiniu.Credentials(sdk.SecretString("test-access-key"), sdk.SecretString("test-secret-key")),
        )
        if recorder.scenario["name"] != "listBuckets-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
        clock=lambda: FIXED_TIME,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match signed requests, outputs, branches, and retries against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
