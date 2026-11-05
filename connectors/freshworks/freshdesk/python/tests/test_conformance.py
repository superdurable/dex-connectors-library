# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Freshdesk provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.freshworks_freshdesk as freshdesk

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/freshworks/freshdesk/scenarios.json")
CONNECTION = sdk.ConnectionRef("freshdesk", "freshdesk-test")
ADAPTER = PythonOperationAdapter(
    "freshdesk",
    CONNECTION,
    freshdesk,
    lambda client, _: freshdesk.Client(
        freshdesk.Config(domain="acme"),
        sdk.StaticCredentialProvider(
            {CONNECTION: freshdesk.Credentials(sdk.SecretString("freshdeskTestKey0123456789"))}
        ),
        client,
        api_base_url="http://127.0.0.1/api/v2",
        now=lambda: datetime(2026, 10, 30, tzinfo=UTC),
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go's provider exchanges, branch output, and heartbeat checkpoints."""
    await replay(scenario, ADAPTER.invoke, "/api/v2")
