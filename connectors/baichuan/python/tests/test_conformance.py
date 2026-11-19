# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Baichuan requests and provider decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.baichuan as baichuan

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/baichuan/scenarios.json")
CONNECTION = sdk.ConnectionRef("baichuan", "baichuan")
ADAPTER = conformance.PythonOperationAdapter(
    "baichuan",
    CONNECTION,
    baichuan,
    lambda http_client, recorder: baichuan.Client(
        baichuan.Config(
            base_url="http://127.0.0.1:12345",
            max_response_bytes=30 if recorder.scenario["name"] == "listKnowledgeBases-oversized" else 1048576,
        ),
        conformance.static_credentials(
            CONNECTION, baichuan.Credentials(sdk.SecretString("test-baichuan-key-123"))
        )
        if recorder.scenario["name"] != "listKnowledgeBases-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, output, branches, and retries against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
