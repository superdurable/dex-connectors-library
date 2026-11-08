# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded GA4 provider decisions in Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.google_analytics as analytics4

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/analytics/scenarios.json")
CONNECTION = sdk.ConnectionRef("google-analytics-4", "google-analytics-4")


def create_client(http_client: httpx.AsyncClient, recorder: object) -> analytics4.Client:
    """Bind inert Go fixture credentials to the replay transport."""
    assert hasattr(recorder, "scenario")
    scenario = recorder.scenario
    assert isinstance(scenario, dict)
    access_token = "" if scenario["name"] == "getProperty-defect" else "test-analytics-token"
    credentials = analytics4.Credentials(
        "test-client",
        sdk.SecretString("test-client-secret"),
        sdk.SecretString(access_token),
        sdk.SecretString("test-refresh-token"),
    )
    return analytics4.Client(
        analytics4.Config(data_base_url="http://127.0.0.1:12345", admin_base_url="http://127.0.0.1:12345"),
        static_credentials(CONNECTION, credentials),
        http_client,
    )


ADAPTER = PythonOperationAdapter(
    "google-analytics-4", CONNECTION, analytics4, create_client, with_context=True
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go."""
    await replay(scenario, ADAPTER.invoke)
