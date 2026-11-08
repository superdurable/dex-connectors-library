# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded GTM provider decisions in Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.google_tag_manager as tagmanager

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/tag-manager/scenarios.json")
CONNECTION = sdk.ConnectionRef("google-tag-manager", "google-tag-manager")


def create_client(http_client: httpx.AsyncClient, recorder: object) -> tagmanager.Client:
    """Bind inert Go fixture credentials to the replay transport."""
    assert hasattr(recorder, "scenario")
    scenario = recorder.scenario
    assert isinstance(scenario, dict)
    access_token = "" if scenario["name"] == "listContainers-defect" else "test-tag-manager-token"
    credentials = tagmanager.Credentials(
        "test-client",
        sdk.SecretString("test-client-secret"),
        sdk.SecretString(access_token),
        sdk.SecretString("test-refresh-token"),
    )
    return tagmanager.Client(
        tagmanager.Config(base_url="http://127.0.0.1:12345"),
        static_credentials(CONNECTION, credentials),
        http_client,
    )


ADAPTER = PythonOperationAdapter(
    "google-tag-manager", CONNECTION, tagmanager, create_client, with_context=True
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go."""
    await replay(scenario, ADAPTER.invoke)
