# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Forms requests and outcomes through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.google_forms as forms

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/google/forms/scenarios.json")
CONNECTION = sdk.ConnectionRef("google", "google-forms-intake")
ADAPTER = conformance.PythonOperationAdapter(
    "google-forms",
    CONNECTION,
    forms,
    lambda client, recorder: forms.Client(
        forms.Config(endpoint="http://127.0.0.1:12345"),
        conformance.static_credentials(
            CONNECTION, forms.Credentials(access_token=sdk.SecretString("forms-token-SENTINEL"))
        ),
        client,
        conformance.NeverRefreshCredentials(recorder),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go's provider requests, branch, failure, and SDK-encoded output."""
    await conformance.replay(scenario, ADAPTER.invoke, "/v1")
