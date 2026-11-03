# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Mailgun requests through the Python connector."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.mailgun as mailgun

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/mailgun/scenarios.json")
CONNECTION = sdk.ConnectionRef("mailgun", "mailgun")
ADAPTER = conformance.PythonOperationAdapter(
    "mailgun", CONNECTION, mailgun,
    lambda http_client, _: mailgun.Client(
        mailgun.Config(base_url="http://127.0.0.1"),
        conformance.static_credentials(
            CONNECTION, mailgun.Credentials(sdk.SecretString("test-key-123"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Mailgun request shapes and decisions recorded by Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
