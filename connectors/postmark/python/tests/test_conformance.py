# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Postmark provider scenarios through the Python package."""

import sys
from pathlib import Path

import dex_connectors.postmark as postmark
import dex_connectors.sdk as sdk
import pytest

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/postmark/scenarios.json")
CONNECTION = sdk.ConnectionRef("postmark", "postmark")
ADAPTER = conformance.PythonOperationAdapter(
    "postmark", CONNECTION, postmark,
    lambda client, _: postmark.Client(
        postmark.Config(base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(CONNECTION, postmark.Credentials(sdk.SecretString("test-secret-token"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and query outcomes against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
