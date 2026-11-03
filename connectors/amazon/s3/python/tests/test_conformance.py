# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay S3's Go-recorded provider scenarios through Python operations."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.amazon_s3 as s3

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/amazon/s3/scenarios.json")
CONNECTION = sdk.ConnectionRef("amazon-s3", "s3-test")


def create_client(_http_client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> s3.Client:
    """Replay fixed-clock SigV4 requests against Go-recorded headers."""

    http_client = httpx.AsyncClient(transport=httpx.MockTransport(recorder.answer))
    credentials = s3.Credentials(
        sdk.SecretString("AKIAS3CONNECTORTEST1"),
        sdk.SecretString("s3-connector-test/secret+value"),
        sdk.SecretString(""),
    )
    return s3.Client(
        s3.Config(endpoint="http://127.0.0.1:12345", default_bucket="acme-reports", region="eu-west-1"),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
        lambda: datetime(2026, 10, 25, 9, tzinfo=UTC),
    )


ADAPTER = conformance.PythonOperationAdapter("amazon-s3", CONNECTION, s3, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match every Go request, output, branch, Retry, and read-back decision."""
    await conformance.replay(scenario, ADAPTER.invoke)
