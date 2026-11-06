# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-verified ClickUp provider exchanges."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.clickup as clickup

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/clickup/scenarios.json")
CONNECTION = sdk.ConnectionRef("clickup", "clickupConnection")


def create_client(http_client, recorder):
    """Construct the connector with inert credentials and the recorder transport."""
    return clickup.Client(
        clickup.Config(),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: clickup.Credentials(
                    sdk.SecretString("pk_4411723_TESTTOKEN0123456789ABCDEFGHIJ"), sdk.SecretString("")
                )
            }
        ),
        http_client,
        now=lambda: datetime.fromtimestamp(1767225600, UTC),
        api_base_url="http://localhost/api/v2",
    )


ADAPTER = PythonOperationAdapter("clickup", CONNECTION, clickup, create_client, with_context=True)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario):
    """Check exact provider exchanges, branches, retries, and checkpoints."""
    await replay(scenario, ADAPTER.invoke, "/api/v2")
