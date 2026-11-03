# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Front provider exchanges through Python operations."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.front as front

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/front/scenarios.json")
CONNECTION = sdk.ConnectionRef("front", "front-test")
ADAPTER = PythonOperationAdapter(
    "front",
    CONNECTION,
    front,
    lambda http_client, _: front.Client(
        front.Config(),
        sdk.StaticCredentialProvider(
            {CONNECTION: front.Credentials(sdk.SecretString("front-unit-test-token-0123456789"))}
        ),
        http_client,
        api_base_url="http://127.0.0.1:12345",
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare every request, branch, retry, and exact SDK output to Go."""
    await replay(scenario, ADAPTER.invoke)
