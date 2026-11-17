# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Enlvon policy search scenarios through Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.enlvon as enlvon

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/enlvon/scenarios.json")
CONNECTION = sdk.ConnectionRef("enlvon", "enlvon")


def new_client(http_client: object, recorder: object) -> enlvon.Client:
    """Supply an inert enl_ key and the scenario-specific response bound."""
    name = recorder.scenario["name"]
    is_missing = name == "searchPolicies-missing-credentials"
    limit = 5 if name == "searchPolicies-oversize" else 2_097_152
    return enlvon.Client(
        enlvon.Config(max_response_bytes=limit),
        conformance.static_credentials(
            CONNECTION,
            enlvon.Credentials(sdk.SecretString("" if is_missing else "enl_test-secret")),
        ),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("enlvon", CONNECTION, enlvon, new_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check query/header, filtered output, branches, Retry, and request counts against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
