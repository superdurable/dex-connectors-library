# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded XiaoeTech category scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.xiaoetech as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/xiaoetech/scenarios.json")
CONNECTION = sdk.ConnectionRef("xiaoetech", "xiaoetech")


def create_client(http_client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Use inert fixture credentials and deterministic signing time."""
    scenario = recorder.scenario
    credentials = connector.Credentials("app-test", "xop-test", sdk.SecretString("REDACTED_SECRET_KEY"))
    if scenario["name"] == "listRootGoodsCategories-defect":
        credentials = connector.Credentials("", "", sdk.SecretString(""))
    return connector.Client(
        connector.Config(
            max_response_bytes=16 if scenario["name"] == "listRootGoodsCategories-oversized" else 2097152
        ),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("xiaoetech", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare request signature, branch, retry, and output with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
