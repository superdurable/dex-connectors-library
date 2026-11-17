# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep private URL material out of reader failures and transport calls."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.dataeyes import DEFECT, Client, Config, Credentials, ReadWebPageInput


@pytest.mark.parametrize(
    "url",
    [
        "http://example.com",
        "https://localhost/a",
        "https://127.0.0.1/a",
        "https://example.com/?token=private-value",
        "https://user:private-value@example.com",
        "https://example.com/#private-value",
        "https://example.com:8443/a",
    ],
)
async def test_private_url_is_rejected_without_dispatch_or_leak(url: str) -> None:
    """Validate locally before transport and expose only a fixed failure message."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={"code": 0, "data": {"markdown": "ignored"}})

    reference = ConnectionRef("dataeyes", "dataeyes")
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(),
            StaticCredentialProvider({reference: Credentials(SecretString("test-secret"))}),
            http_client,
        )
        attempt = await client.read_web_page().invoke(
            Call(CallID("reader-security"), reference, OperationRef("dataeyes", "readWebPage")),
            ReadWebPageInput(url),
        )
    assert attempt.branch == DEFECT
    assert attempt.failure is not None
    assert url not in attempt.failure.message
    assert "private-value" not in attempt.failure.message
    assert requests == []
