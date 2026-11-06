# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Google Drive requests through the Python SDK."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.google_drive as drive

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/drive/scenarios.json")
CONNECTION = sdk.ConnectionRef("google", "drive-files")
ADAPTER = PythonOperationAdapter(
    "google-drive",
    CONNECTION,
    drive,
    lambda http_client, _: drive.Client(
        drive.Config(endpoint="http://127.0.0.1:12345", max_text_bytes=8),
        sdk.StaticCredentialProvider(
            {CONNECTION: drive.Credentials(access_token=sdk.SecretString("drive-token-SENTINEL"))}
        ),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, retries, branches, and provider outputs."""
    await replay(scenario, ADAPTER.invoke)
