# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded vast_tripo provider scenarios."""

import sys
from pathlib import Path
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.vast_tripo as connector

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/vast/tripo/scenarios.json")
CONNECTION = sdk.ConnectionRef("tripo", "tripo")


def make_client(http_client: httpx.AsyncClient, recorder: Any) -> connector.Client:
    """Supply the fixture's inert credentials and deterministic signed timestamp."""
    defect = bool(recorder.scenario["name"].endswith("-defect"))
    credentials = connector.Credentials(sdk.SecretString("" if defect else "test-api-key"))
    return connector.Client(
        connector.Config(max_response_bytes=8)
        if recorder.scenario["name"].endswith("-oversize")
        else connector.Config(),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("tripo", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact Go requests, outputs, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
