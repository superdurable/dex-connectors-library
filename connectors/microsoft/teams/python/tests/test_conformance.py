# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Microsoft Teams provider scenarios."""

import sys
from datetime import datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.microsoft_teams as teams

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/microsoft/teams/scenarios.json")
CONNECTION = sdk.ConnectionRef("microsoft", "teams-test")


def create_client(http_client, recorder):
    """Create a Graph client with inert OAuth material and a fixed clock."""
    return teams.Client(
        teams.Config(endpoint="http://localhost/v1.0"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: teams.Credentials(
                    "client-id",
                    sdk.SecretString("client-secret"),
                    sdk.SecretString("teams-access-token"),
                    sdk.SecretString("refresh-token"),
                )
            }
        ),
        http_client,
        now=lambda: datetime.fromisoformat(recorder.scenario.get("fixedClock", "2026-10-01T09:30:00Z")),
    )


ADAPTER = PythonOperationAdapter("microsoft-teams", CONNECTION, teams, create_client, with_context=True)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario):
    """Match Go requests, branches, retries, and dispatch checkpoints."""
    await replay(scenario, ADAPTER.invoke, "/v1.0")
