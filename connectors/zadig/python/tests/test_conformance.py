# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Zadig provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.zadig as provider

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/zadig/scenarios.json")
CONNECTION = sdk.ConnectionRef("zadig", "zadig")


def make_client(client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> provider.Client:
    """Build the provider client from synthetic Go scenario settings."""
    token = "" if recorder.scenario["name"].endswith("-defect") else "private-test-key"
    return provider.Client(
        provider.Config(
            base_url="https://zadig.example",
            max_response_bytes=5 if recorder.scenario["name"].endswith("-oversized") else 1048576,
        ),
        conformance.static_credentials(CONNECTION, provider.Credentials(sdk.SecretString(token))),
        client,
    )


ADAPTER = conformance.PythonOperationAdapter("zadig", CONNECTION, provider, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests and SDK results with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
