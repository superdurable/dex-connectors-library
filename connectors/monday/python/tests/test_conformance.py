# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded monday.com requests and outcomes through the Python SDK."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.monday as monday

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/monday/scenarios.json")
CONNECTION = sdk.ConnectionRef("monday", "monday-test")
ADAPTER = PythonOperationAdapter(
    "monday",
    CONNECTION,
    monday,
    lambda http_client, _: monday.Client(
        monday.Config(),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: monday.Credentials(
                    "personal-api-token", sdk.SecretString("mondayTestToken0123456789abcdefSENTINELKEY")
                )
            }
        ),
        http_client,
        endpoint="http://127.0.0.1:45101/v2",
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match every request, branch, retry, uncertainty, and checkpoint to Go."""
    await replay(scenario, ADAPTER.invoke, "")
