# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Caiyun provider requests and decisions."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.caiyun as caiyun

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/caiyun/scenarios.json")
CONNECTION = sdk.ConnectionRef("caiyun", "caiyun")
_DATE = datetime(2026, 10, 22, 7, 26, 40, tzinfo=UTC)
_NONCE = bytes.fromhex("0195c68a42e77243bff2ac97a78b837d")
ADAPTER = conformance.PythonOperationAdapter(
    "caiyun",
    CONNECTION,
    caiyun,
    lambda http_client, recorder: caiyun.Client(
        caiyun.Config(
            base_url="http://127.0.0.1:12345",
            max_response_bytes=8 if recorder.scenario["name"] == "getCurrentWeather-oversized" else 262144,
        ),
        conformance.static_credentials(
            CONNECTION,
            caiyun.Credentials(sdk.SecretString("test-app-key"), sdk.SecretString("test-app-secret")),
        )
        if recorder.scenario["name"] != "getCurrentWeather-missing-credentials"
        else sdk.StaticCredentialProvider({}),
        http_client,
        clock=lambda: _DATE,
        nonce_source=lambda length: _NONCE,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, outputs, branches, retries, and request counts."""
    await conformance.replay(scenario, ADAPTER.invoke)
