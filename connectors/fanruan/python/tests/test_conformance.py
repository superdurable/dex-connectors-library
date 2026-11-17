# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Jiandaoyun application-page scenarios through Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.fanruan as fanruan

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/fanruan/scenarios.json")
CONNECTION = sdk.ConnectionRef("fanruan", "fanruan")


def new_client(http_client: object, recorder: object) -> fanruan.Client:
    """Supply an inert API Key and scenario-specific response bound."""
    name = recorder.scenario["name"]
    is_missing = name == "listAppsPage-missing-credentials"
    limit = 16 if name == "listAppsPage-oversize" else 1_048_576
    return fanruan.Client(
        fanruan.Config(max_response_bytes=limit),
        conformance.static_credentials(
            CONNECTION,
            fanruan.Credentials(sdk.SecretString("" if is_missing else "secret-api-key")),
        ),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("fanruan", CONNECTION, fanruan, new_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check request, filtered output, branches, Retry, and request counts against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
