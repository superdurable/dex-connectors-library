# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Marketo lead scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.adobe_marketo as marketo

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/adobe/marketo/scenarios.json")
CONNECTION = sdk.ConnectionRef("adobe", "adobe-marketo")
TEST_CREDENTIALS = marketo.Credentials(
    "sample-client-id", sdk.SecretString("sample-client-secret"), sdk.SecretString("sample-access-token")
)
ADAPTER = conformance.PythonOperationAdapter(
    "adobe-marketo",
    CONNECTION,
    marketo,
    lambda http_client, _recorder: marketo.Client(
        marketo.Config(
            rest_endpoint="https://123-ABC-456.mktorest.com/rest",
            token_endpoint="https://123-ABC-456.mktorest.com/identity/oauth/token",
        ),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/rest")
