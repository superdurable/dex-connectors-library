# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Oceanbase requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.oceanbase as provider

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/oceanbase/scenarios.json")
CONNECTION = sdk.ConnectionRef("oceanbase", "oceanbase")


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> provider.Client:
    """Bind inert fixture credentials, including the missing-credential case."""
    import httpx

    assert isinstance(http_client, httpx.AsyncClient)
    import hashlib
    from datetime import datetime

    credentials = provider.Credentials(
        sdk.SecretString("REDACTED_OCEANBASE_ACCESS_KEY"),
        sdk.SecretString("REDACTED_OCEANBASE_SECRET"),
        sdk.SecretString("REDACTED_PROJECT_ID"),
    )
    fixed_clock = datetime.fromisoformat(recorder.scenario["fixedClock"].replace("Z", "+00:00"))
    counter = 0
    offset = 32
    block = b""

    def random_bytes() -> bytes:
        nonlocal counter, offset, block
        result = bytearray()
        for _ in range(16):
            if offset == 32:
                block = hashlib.sha256(
                    recorder.scenario["randomSeed"].encode() + counter.to_bytes(8, "big")
                ).digest()
                counter += 1
                offset = 0
            result.append(block[offset])
            offset += 1
        return bytes(result)

    site = "international" if "international" in recorder.scenario["name"] else "domestic"
    return provider.Client(
        provider.Config(site=site),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
        clock=lambda: fixed_clock,
        random_bytes=random_bytes,
    )


ADAPTER = conformance.PythonOperationAdapter("oceanbase", CONNECTION, provider, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match every Go request, output, branch, and retry decision exactly."""
    await conformance.replay(scenario, ADAPTER.invoke)
