# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded SendGrid exchanges through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.sendgrid as sendgrid

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/sendgrid/scenarios.json")
CONNECTION = sdk.ConnectionRef("sendgrid", "sendgrid")
ADAPTER = PythonOperationAdapter(
    "sendgrid",
    CONNECTION,
    sendgrid,
    lambda http_client, _: sendgrid.Client(
        sendgrid.Config(),
        sdk.StaticCredentialProvider({CONNECTION: sendgrid.Credentials(sdk.SecretString("test-key-123"))}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests and attempt outcomes with the Go recording."""
    await replay(scenario, ADAPTER.invoke)
