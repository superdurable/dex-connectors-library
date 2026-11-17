# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Aqara signed requests and provider decisions."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.aqara as aqara

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/aqara/scenarios.json")
CONNECTION = sdk.ConnectionRef("aqara", "aqara")
_NONCE = bytes.fromhex("3156853299f313e23dc12e1703dc12e1")
_DATE = datetime(2023, 10, 26, 10, 22, 32, tzinfo=UTC)
ADAPTER = conformance.PythonOperationAdapter(
    "aqara",
    CONNECTION,
    aqara,
    lambda http_client, recorder: aqara.Client(
        aqara.Config(
            base_url="http://127.0.0.1:12345",
            max_response_bytes=4 if recorder.scenario["name"] == "listDevices-oversized" else 1048576,
        ),
        conformance.static_credentials(
            CONNECTION,
            aqara.Credentials(
                "app-id", "key-id", sdk.SecretString("app-key"), sdk.SecretString("access-token")
            ),
        ),
        http_client,
        clock=lambda: _DATE,
        nonce_source=lambda length: _NONCE,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match signed requests, exact outputs, branches, and retries against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)


def test_official_signing_vector() -> None:
    """Keep Aqara's documented MD5 header signature stable."""
    from dex_connectors.aqara.client import _sign_request

    assert (
        _sign_request(
            "532cad73c5493193d63d367016b98b27",
            "4e693d54d75db580a56d1263",
            "78784564654feda454557",
            "C6wuzd0Qguxzelhb",
            "1618914078668",
            "gU7Qtxi4dWnYAdmudyxni52bWZ58b8uN",
        )
        == "bfd8dd0e7c108353e6740d81e05982d8"
    )
