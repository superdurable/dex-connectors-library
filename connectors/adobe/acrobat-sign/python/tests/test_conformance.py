# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded AcrobatSign provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.adobe_acrobat_sign as acrobat_sign

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/adobe/acrobat-sign/scenarios.json")
CONNECTION = sdk.ConnectionRef("adobe-acrobat-sign", "adobe-acrobat-sign")
TEST_CREDENTIALS = acrobat_sign.Credentials(sdk.SecretString("sample-acrobat-sign-integration-key"))
ADAPTER = conformance.PythonOperationAdapter(
    "adobe-acrobat-sign",
    CONNECTION,
    acrobat_sign,
    lambda http_client, _recorder: acrobat_sign.Client(
        acrobat_sign.Config(base_url="http://127.0.0.1:12345/api/rest/v6"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/api/rest/v6")
