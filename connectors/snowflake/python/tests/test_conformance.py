# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-verified Snowflake SQL API scenarios through the Python SDK."""

import base64
import hashlib
import json
import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.hazmat.primitives.hashes import SHA256

import dex_connectors.snowflake as snowflake

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/snowflake/scenarios.json")
CONNECTION = sdk.ConnectionRef("snowflake", "warehouse")


def create_client(http_client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> snowflake.Client:
    """Use the scenario's authentication method and verify key-pair JWTs before replay."""
    credentials = snowflake.Credentials(
        auth_method_id="programmatic-access-token",
        programmatic_access_token=sdk.SecretString("pat-secret-value-0123456789"),
    )
    if recorder.scenario["name"] == "submit-key-pair-jwt":
        key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        private_key = key.private_bytes(
            serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        ).decode()
        credentials = snowflake.Credentials(
            auth_method_id="key-pair", user="dex_service",
            private_key=sdk.SecretString(private_key),
        )

        async def verify_jwt(request: httpx.Request) -> None:
            token = request.headers["Authorization"].removeprefix("Bearer ")
            header, claims, signature = token.split(".")
            def decode(value: str) -> bytes:
                return base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))
            assert json.loads(decode(header)) == {"alg": "RS256", "typ": "JWT"}
            payload = json.loads(decode(claims))
            qualified = "MYORG-ANALYTICS.DEX_SERVICE"
            fingerprint = hashlib.sha256(key.public_key().public_bytes(
                serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo,
            )).digest()
            assert payload == {
                "iss": qualified + ".SHA256:" + base64.b64encode(fingerprint).decode(),
                "sub": qualified, "iat": 1790769600, "exp": 1790773140,
            }
            key.public_key().verify(decode(signature), (header + "." + claims).encode(),
                                    padding.PKCS1v15(), SHA256())
            request.headers["Authorization"] = "Bearer conformance-key-pair-jwt"

        http_client.event_hooks["request"].append(verify_jwt)

    return snowflake.Client(
        snowflake.Config(
            account_identifier="myorg-analytics", warehouse="REPORTING_WH", role="DEX_REPORTING",
            database="ANALYTICS", schema="BILLING", max_rows=1,
        ),
        conformance.static_credentials(CONNECTION, credentials), http_client,
        local_provider_url="http://127.0.0.1",
        now=lambda: datetime(2026, 9, 30, 12, 0, tzinfo=UTC),
    )


ADAPTER = conformance.PythonOperationAdapter(
    "snowflake",
    CONNECTION,
    snowflake,
    create_client,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare each operation's exact provider exchange and decoded outcome with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
