# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded private-safe EasyAPI shop queries through Python."""

import json
import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.easyapi as easyapi

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/easyapi/scenarios.json")
CONNECTION = sdk.ConnectionRef("easyapi", "easyapi")


def new_client(http_client: object, recorder: object) -> easyapi.Client:
    """Supply inert credentials and the scenario-specific response bound."""
    name = recorder.scenario["name"]
    is_missing = name == "queryShop-missing-credentials"
    limit = 5 if name == "queryShop-oversize" else 262_144
    return easyapi.Client(
        easyapi.Config(base_url="https://fapiao-api.easyapi.com", max_response_bytes=limit),
        conformance.static_credentials(
            CONNECTION,
            easyapi.Credentials(
                sdk.SecretString("" if is_missing else "test-key"),
                sdk.SecretString("" if is_missing else "private-secret"),
            ),
        ),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("easyapi", CONNECTION, easyapi, new_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check private query params, outputs, branches, Retry, and request counts against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
    recorder = conformance.ExchangeRecorder(scenario, scenario["callId"])
    context = conformance.HeartbeatContext(None, None, scenario["name"])
    attempt = await ADAPTER.invoke(scenario, recorder, context)
    recorder.assert_complete()
    for private_value in (
        "test-key", "private-secret", "91320211MA1WML8X6T", "private tax",
        "private address", "private phone", "private bank", "private staff",
        "private certificate",
    ):
        assert private_value not in repr(attempt)


def test_recorded_outcomes_exclude_private_values() -> None:
    """Neither a Go result nor failure record retains credentials or enterprise details."""
    forbidden = (
        "test-key", "private-secret", "91320211MA1WML8X6T", "private tax",
        "private address", "private phone", "private bank", "private staff",
        "private certificate",
    )
    for scenario in SCENARIOS:
        expected = json.dumps(scenario["expected"], ensure_ascii=False)
        for private_value in forbidden:
            assert private_value not in expected
