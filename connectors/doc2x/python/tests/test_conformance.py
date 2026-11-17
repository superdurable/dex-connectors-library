# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Doc2X provider scenarios through the Python Query."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.doc2x as doc2x

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/doc2x/scenarios.json")
CONNECTION = sdk.ConnectionRef("doc2x", "doc2x")
ADAPTER = conformance.PythonOperationAdapter(
    "doc2x",
    CONNECTION,
    doc2x,
    lambda http_client, recorder: doc2x.Client(
        doc2x.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "getParseStatus-oversize" else 1048576
        ),
        conformance.static_credentials(CONNECTION, doc2x.Credentials(sdk.SecretString("test-secret")))
        if recorder.scenario["name"] != "getParseStatus-missing-key"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, outputs, branches, and retry decisions against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
