# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Google Search Console provider decisions in Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.google_search_console as google_search_console

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/search-console/scenarios.json")
CONNECTION = sdk.ConnectionRef("google-search-console", "google-search-console")


def create_client(http_client: httpx.AsyncClient, recorder: object) -> google_search_console.Client:
    """Bind the inert Go fixture credentials to the replay transport."""
    assert hasattr(recorder, "scenario")
    scenario = recorder.scenario
    assert isinstance(scenario, dict)
    access_token = "" if scenario["name"] == "listSites-defect" else "test-search-console-token"
    credentials = google_search_console.Credentials(
        "test-client",
        sdk.SecretString("test-client-secret"),
        sdk.SecretString(access_token),
        sdk.SecretString("test-refresh-token"),
    )
    return google_search_console.Client(
        google_search_console.Config(base_url="http://127.0.0.1:12345"),
        static_credentials(CONNECTION, credentials),
        http_client,
    )


ADAPTER = PythonOperationAdapter(
    "google-search-console", CONNECTION, google_search_console, create_client, with_context=True
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, heartbeats, and decisions with Go."""
    await replay(scenario, ADAPTER.invoke)
