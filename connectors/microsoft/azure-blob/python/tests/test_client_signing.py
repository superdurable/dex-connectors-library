# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Shared Key signing uses the encoded request path."""

import base64
import hashlib
import hmac
from datetime import UTC, datetime

import dex_connectors.sdk as sdk
import httpx

import dex_connectors.microsoft_azure_blob as microsoft_azure_blob

CONNECTION = sdk.ConnectionRef("microsoft-azure-blob", "microsoft-azure-blob")
ENCODED_PATH = "/docs/reports/q1%20r%C3%A9sum%C3%A9.txt"


def _expected_authorization() -> str:
    signed_value = (
        "GET"
        + "\n" * 12
        + "x-ms-date:Fri, 30 Oct 2026 00:00:00 GMT\nx-ms-version:2023-11-03\n"
        + "/exampleacct"
        + ENCODED_PATH
    )
    digest = hmac.new(b"secret-test-key", signed_value.encode(), hashlib.sha256).digest()
    return "SharedKey exampleacct:" + base64.b64encode(digest).decode()


async def test_get_blob_signs_encoded_request_path() -> None:
    """Sign the same percent-encoded path that httpx sends."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, content=b"report", headers={"Content-Type": "text/plain"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = microsoft_azure_blob.Client(
            microsoft_azure_blob.Config(
                endpoint="https://exampleacct.blob.core.windows.net", account_name="exampleacct"
            ),
            sdk.StaticCredentialProvider(
                {CONNECTION: microsoft_azure_blob.Credentials(sdk.SecretString("c2VjcmV0LXRlc3Qta2V5"))}
            ),
            http_client,
            now=lambda: datetime(2026, 10, 30, tzinfo=UTC),
        )
        call = sdk.Call(
            sdk.CallID("azure-blob-signing-test"),
            CONNECTION,
            sdk.OperationRef("microsoft-azure-blob", "getBlob"),
            sdk.IdempotencyKey(""),
        )
        attempt = await client.get_blob().invoke(
            call, microsoft_azure_blob.GetBlobInput(container="docs", blob_name="reports/q1 résumé.txt")
        )
    assert attempt.branch == "downloaded"
    assert len(requests) == 1
    assert requests[0].url.raw_path.decode() == ENCODED_PATH
    assert requests[0].url.path == "/docs/reports/q1 résumé.txt"
    assert requests[0].headers["Authorization"] == _expected_authorization()
