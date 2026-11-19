# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Youzan provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.youzan as youzan

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/youzan/scenarios.json")
CONNECTION = sdk.ConnectionRef("youzan", "youzan")
ADAPTER = conformance.PythonOperationAdapter(
    "youzan",
    CONNECTION,
    youzan,
    lambda http_client, recorder: youzan.Client(
        youzan.Config(
            max_response_bytes=8 if recorder.scenario["name"] == "getShopBasic-oversized" else 65536
        ),
        conformance.static_credentials(CONNECTION, youzan.Credentials(sdk.SecretString("sample-test-token")))
        if recorder.scenario["name"] != "getShopBasic-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match the Go recording's requests, branches, outputs, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
