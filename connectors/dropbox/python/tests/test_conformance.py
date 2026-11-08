# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-verified Dropbox requests and provider decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.dropbox as dropbox

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/dropbox/scenarios.json")
CONNECTION = sdk.ConnectionRef("dropbox", "dropbox")


def create_client(http_client: object, recorder: object) -> dropbox.Client:
    """Bind the exact Go fixture credential to an isolated provider transport."""
    import httpx

    assert isinstance(http_client, httpx.AsyncClient)
    assert hasattr(recorder, "scenario")
    scenario = recorder.scenario
    assert isinstance(scenario, dict)
    access_token = "" if scenario["name"] == "listFolder-defect" else "test-token"
    credentials = dropbox.Credentials(
        "app-key",
        sdk.SecretString("app-secret"),
        sdk.SecretString(access_token),
        sdk.SecretString("refresh-token"),
    )
    return dropbox.Client(
        dropbox.Config(
            base_url="http://127.0.0.1",
            content_base_url="http://127.0.0.1",
            max_download_bytes=8,
            max_upload_bytes=8,
        ),
        static_credentials(CONNECTION, credentials),
        http_client,
    )


ADAPTER = PythonOperationAdapter("dropbox", CONNECTION, dropbox, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Require Python branches, retries, outputs, and requests to match Go."""
    await replay(scenario, ADAPTER.invoke)
