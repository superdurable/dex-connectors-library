# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Qingflow provider scenarios recorded by Go."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.qingflow as qingflow

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/qingflow/scenarios.json")
CONNECTION = sdk.ConnectionRef("qingflow", "qingflow")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> qingflow.Client:
    """Build the Query with the scenario's synthetic token."""
    token = "" if recorder.scenario["name"] == "listApplications-defect" else "private-test-key"
    return qingflow.Client(
        qingflow.Config(),
        conformance.static_credentials(CONNECTION, qingflow.Credentials(sdk.SecretString(token))),
        client,
    )


ADAPTER = conformance.PythonOperationAdapter("qingflow", CONNECTION, qingflow, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, outputs, branch decisions, and retry counts with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
