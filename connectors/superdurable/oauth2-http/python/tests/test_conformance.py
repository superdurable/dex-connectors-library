# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded OAuth HTTP provider scenarios through the Python Mutation."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.superdurable_oauth2_http as oauth2_http

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(
    ROOT / "conformance/connectors/superdurable/oauth2-http/scenarios.json"
)
CONNECTION = sdk.ConnectionRef("oauth2-http", "oauth2-http")
ADAPTER = conformance.PythonOperationAdapter(
    "oauth2-http",
    CONNECTION,
    oauth2_http,
    lambda http_client, recorder: oauth2_http.Client(
        oauth2_http.Config(
            authorization_endpoint="http://127.0.0.1:12345/auth",
            token_endpoint="http://127.0.0.1:12345/token",
            scopes=("profile",),
            api_base_url="http://127.0.0.1:12345/v1",
        ),
        conformance.static_credentials(
            CONNECTION,
            oauth2_http.Credentials(
                "client",
                sdk.SecretString("client-secret"),
                sdk.SecretString("conformance-access-token"),
                sdk.SecretString(""),
            ),
        ),
        http_client,
        now=lambda: datetime.fromisoformat(
            recorder.scenario.get("fixedClock", "1970-01-01T00:00:01Z")
        ).astimezone(UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Require exact Go-recorded request, branch, retry, and output parity."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/v1")
