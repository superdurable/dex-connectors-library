# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded postmark provider exchanges."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.postmark as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/postmark/scenarios.json")
CONNECTION = sdk.ConnectionRef("postmark", "postmark")
ADAPTER = conformance.PythonOperationAdapter(
    "postmark",
    CONNECTION,
    connector,
    lambda http_client, recorder: connector.Client(
        connector.Config(base_url="http://127.0.0.1:12345"),
        conformance.static_credentials(
            CONNECTION, connector.Credentials(api_key=sdk.SecretString("test-secret-token"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests and SDK outcomes with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
