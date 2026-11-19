# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""uploadObject never refuses an object that an earlier attempt may have uploaded."""

from types import SimpleNamespace
from typing import Any, cast

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

from dex_connectors.google_cloud_storage import Client, Config, Credentials, UploadObjectInput

CONNECTION = ConnectionRef("google-cloud-storage", "google-cloud-storage")
INPUT = UploadObjectInput(
    bucket="example-bucket", name="a", content_base64="aGVsbG8=", content_type="text/plain"
)


def handle(request: httpx.Request) -> httpx.Response:
    """Report the name as taken and deny reading its bytes."""
    if request.method == "POST":
        return httpx.Response(412)
    if request.url.params.get("alt") == "media":
        return httpx.Response(403)
    return httpx.Response(200, content=b'{"name":"a","generation":"123"}')


def call_at(attempt: int) -> Call:
    """Build an uploadObject Call for one Step attempt."""
    return Call(
        CallID("0b8c3e57-1d2a-4f6e-8a9b-3c4d5e6f7a81"),
        CONNECTION,
        OperationRef("google-cloud-storage", "uploadObject"),
        context=cast(Any, SimpleNamespace(attempt=attempt)),
    )


@pytest.mark.asyncio
@pytest.mark.parametrize(("attempt", "expected_kind"), [(1, "branch"), (2, "retry")])
async def test_unreadable_existing_object_is_refused_only_on_a_first_attempt(
    attempt: int, expected_kind: str
) -> None:
    """A first attempt is refused; a later one retries, because the object may be its own."""
    credentials = Credentials(
        "client", SecretString("secret"), SecretString("token"), SecretString("refresh")
    )
    client = Client(
        Config(),
        StaticCredentialProvider({CONNECTION: credentials}),
        httpx.AsyncClient(transport=httpx.MockTransport(handle)),
    )
    result = await client.upload_object().invoke(call_at(attempt), INPUT)
    assert result.kind == expected_kind
    if expected_kind == "branch":
        assert result.branch == "providerRejected"
    await client.aclose()
