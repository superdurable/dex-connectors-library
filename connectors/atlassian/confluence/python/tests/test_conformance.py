# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-verified Confluence provider exchanges through Python operations."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.atlassian_confluence as confluence

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/atlassian/confluence/scenarios.json")
CONNECTION = sdk.ConnectionRef("atlassian", "confluence-site")
ADAPTER = PythonOperationAdapter(
    "confluence",
    CONNECTION,
    confluence,
    lambda transport, recorder: confluence.Client(
        confluence.Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789", endpoint="http://127.0.0.1"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: confluence.Credentials(
                    "confluence-test-client",
                    sdk.SecretString("confluence-test-client-secret"),
                    sdk.SecretString("confluence-access-token"),
                    sdk.SecretString("confluence-test-refresh-token"),
                )
            }
        ),
        transport,
        lambda: datetime(2026, 10, 25, 9, tzinfo=UTC),
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go's requests, branches, retries, receipts, and heartbeats."""
    await replay(scenario, ADAPTER.invoke)
