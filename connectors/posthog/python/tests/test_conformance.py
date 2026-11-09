# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay all Go-recorded PostHog provider requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.posthog as posthog

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/posthog/scenarios.json")
CONNECTION = sdk.ConnectionRef("posthog", "posthog")
ADAPTER = PythonOperationAdapter(
    "posthog",
    CONNECTION,
    posthog,
    lambda http_client, _: posthog.Client(
        posthog.Config(
            capture_url="http://127.0.0.1:12345",
            app_url="http://127.0.0.1:12345",
        ),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: posthog.Credentials(
                    sdk.SecretString("phc_synthetic-token"),
                    sdk.SecretString("phx_synthetic-key"),
                )
            }
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions to exact Go recordings."""
    await replay(scenario, ADAPTER.invoke)
