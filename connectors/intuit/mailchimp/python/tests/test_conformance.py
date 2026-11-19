# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Mailchimp provider scenarios against the Python port."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.intuit_mailchimp as mailchimp

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/intuit/mailchimp/scenarios.json")
CONNECTION = sdk.ConnectionRef("mailchimp", "mailchimp-test")
ADAPTER = PythonOperationAdapter(
    "mailchimp",
    CONNECTION,
    mailchimp,
    lambda http_client, _: mailchimp.Client(
        mailchimp.Config(),
        sdk.StaticCredentialProvider(
            {CONNECTION: mailchimp.Credentials(sdk.SecretString("mailchimpfixturekey-us6"))}
        ),
        http_client,
        api_base_url="http://localhost/3.0",
        now=lambda: datetime(2026, 10, 30, tzinfo=UTC),
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check requests, branches, retries, uncertainty, and exact SDK output."""
    await replay(scenario, ADAPTER.invoke, "/3.0")
