# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded App Token and user request sequences through Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.easemob as easemob

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/easemob/scenarios.json")
CONNECTION = sdk.ConnectionRef("easemob", "easemob")


def new_client(http_client: object, recorder: object) -> easemob.Client:
    """Supply inert application credentials and scenario-specific response bounds."""
    name = recorder.scenario["name"]
    is_missing = name == "getUserDetail-missing-credentials"
    limit = 120 if name == "getUserDetail-oversize-user" else 65_536
    return easemob.Client(
        easemob.Config(base_url="https://app.example.test", max_response_bytes=limit),
        conformance.static_credentials(
            CONNECTION,
            easemob.Credentials(
                "" if is_missing else "demo#chat",
                "" if is_missing else "client-id",
                sdk.SecretString("" if is_missing else "client-secret"),
            ),
        ),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("easemob", CONNECTION, easemob, new_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check token and user requests, refresh, outputs, branches, and Retry against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
