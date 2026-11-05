# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check S3 signing and configuration boundaries not visible in replay headers."""

from datetime import UTC, datetime

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.amazon_s3 import Client, Config, Credentials


@pytest.mark.asyncio
async def test_signature_matches_go_signer() -> None:
    """Use Go's fixed SigV4 reference for an object GET."""
    observed: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        observed.append(request)
        return httpx.Response(200)

    connection = ConnectionRef("amazon-s3", "s3-test")
    credentials = Credentials(
        SecretString("AKIAS3CONNECTORTEST1"),
        SecretString("s3-connector-test/secret+value"),
        SecretString(""),
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(endpoint="http://127.0.0.1:12345", default_bucket="acme-reports", region="eu-west-1"),
            StaticCredentialProvider({connection: credentials}),
            http_client,
            lambda: datetime(2026, 10, 25, 9, tzinfo=UTC),
        )
        call = Call(
            CallID("5908a179-2147-5839-9c64-529cd36f77d7"),
            connection,
            OperationRef("amazon-s3", "getObjectText"),
            IdempotencyKey(""),
        )
        session = await client.start_session(call)
        assert session is not None
        await client.exchange(
            call, session, "GET", "acme-reports", "reports/weekly.md", query={"max-keys": "3"}
        )
        await client.exchange(
            call,
            session,
            "PUT",
            "acme-reports",
            "reports/weekly.md",
            headers={
                "content-type": "text/plain",
                "content-md5": "sZRqySSS0jR8YjW00mERhA==",
                "x-amz-meta-dex-idempotency-key": "0c58ff90-76a5-5ab6-9099-092233c989ba",
                "if-none-match": "*",
            },
            body=b"hello\n",
        )
        await client.exchange(call, session, "GET", "acme-reports", "a b+c/é.txt")
    assert observed[0].headers["authorization"] == (
        "AWS4-HMAC-SHA256 Credential=AKIAS3CONNECTORTEST1/20261025/eu-west-1/s3/aws4_request, "
        "SignedHeaders=host;x-amz-content-sha256;x-amz-date, "
        "Signature=b55faa0643e0e719465dc3cffa4f3052f1a394ae1580a13415befb1917f250fd"
    )
    assert "s3-connector-test/secret+value" not in str(observed[0])
    assert observed[1].headers["authorization"] == (
        "AWS4-HMAC-SHA256 Credential=AKIAS3CONNECTORTEST1/20261025/eu-west-1/s3/aws4_request, "
        "SignedHeaders=content-length;content-md5;content-type;host;if-none-match;"
        "x-amz-content-sha256;x-amz-date;x-amz-meta-dex-idempotency-key, "
        "Signature=9f8d15f7a0330672fe67a13dae8fa52ee97b1b79652cb6a7c86eb70091b41b1e"
    )
    assert str(observed[2].url) == "http://127.0.0.1:12345/acme-reports/a%20b%2Bc/%C3%A9.txt"


def test_invalid_connection_configuration_fails_before_request() -> None:
    """Reject a nonregional default endpoint and an invalid bucket."""
    connection = ConnectionRef("amazon-s3", "s3-test")
    credentials = StaticCredentialProvider(
        {
            connection: Credentials(
                SecretString("AKIAS3CONNECTORTEST1"), SecretString("safe-secret"), SecretString("")
            )
        }
    )
    with pytest.raises(ValueError):
        Client(Config(endpoint="", default_bucket="acme-reports", region="auto"), credentials)
    with pytest.raises(ValueError):
        Client(
            Config(endpoint="http://127.0.0.1:12345", default_bucket="BAD", region="eu-west-1"), credentials
        )


@pytest.mark.asyncio
async def test_default_endpoint_port_and_empty_put_signing() -> None:
    """Sign the transmitted Host and omit zero Content-Length from SigV4."""
    observed: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        observed.append(request)
        return httpx.Response(200)

    connection = ConnectionRef("amazon-s3", "s3-test")
    credentials = StaticCredentialProvider(
        {
            connection: Credentials(
                SecretString("AKIAS3CONNECTORTEST1"), SecretString("safe-secret"), SecretString("")
            )
        }
    )
    call = Call(
        CallID("5908a179-2147-5839-9c64-529cd36f77d7"),
        connection,
        OperationRef("amazon-s3", "putObject"),
        IdempotencyKey(""),
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        for endpoint in ("https://example.com:443", "https://example.com"):
            client = Client(
                Config(endpoint=endpoint, default_bucket="acme-reports", region="eu-west-1"),
                credentials,
                http_client,
                lambda: datetime(2026, 10, 25, 9, tzinfo=UTC),
            )
            session = await client.start_session(call)
            assert session is not None
            await client.exchange(call, session, "PUT", "acme-reports", "empty.txt", body=b"")
    assert observed[0].headers["host"] == "example.com"
    assert observed[0].headers["authorization"] == observed[1].headers["authorization"]
    assert "content-length" not in observed[0].headers["authorization"]
