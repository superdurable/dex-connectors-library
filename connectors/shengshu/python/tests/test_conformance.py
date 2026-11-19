# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Vidu model-list scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.shengshu as shengshu

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/shengshu/scenarios.json")
CONNECTION = sdk.ConnectionRef("shengshu", "shengshu")
ADAPTER = conformance.PythonOperationAdapter(
    "shengshu",
    CONNECTION,
    shengshu,
    lambda http_client, recorder: shengshu.Client(
        shengshu.Config(),
        conformance.static_credentials(CONNECTION, shengshu.Credentials(sdk.SecretString("secret-api-key")))
        if recorder.scenario["name"] != "getTaskStatus-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare model-list requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
