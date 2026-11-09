# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded DropboxSign provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.dropbox_sign as dropbox_sign

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/dropbox/sign/scenarios.json")
CONNECTION = sdk.ConnectionRef("dropbox-sign", "dropbox-sign")
TEST_CREDENTIALS = dropbox_sign.Credentials(sdk.SecretString("sample-dropbox-sign-api-key"))
ADAPTER = conformance.PythonOperationAdapter(
    "dropbox-sign",
    CONNECTION,
    dropbox_sign,
    lambda http_client, _recorder: dropbox_sign.Client(
        dropbox_sign.Config(base_url="http://127.0.0.1:8848/v3"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/v3")
