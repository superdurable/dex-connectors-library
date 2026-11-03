# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Asana provider exchanges through Python SDK calls."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.asana as asana

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/asana/scenarios.json")
CONNECTION = sdk.ConnectionRef("asana", "asana-tasks")
ADAPTER = PythonOperationAdapter(
    "asana",
    CONNECTION,
    asana,
    lambda http_client, _: asana.Client(
        asana.Config(endpoint="http://127.0.0.1:22201/api/1.0"),
        sdk.StaticCredentialProvider({CONNECTION: asana.Credentials(sdk.SecretString("asana-access-token"))}),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match each exact request, provider decision, and SDK-encoded output."""
    await replay(scenario, ADAPTER.invoke, "/api/1.0")
