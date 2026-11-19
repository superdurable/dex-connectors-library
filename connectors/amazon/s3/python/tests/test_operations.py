# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check S3 response parsing and input validation at the provider boundary."""

from datetime import UTC, datetime

import httpx
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
from dex_connectors.amazon_s3.client import Response, Session
from dex_connectors.amazon_s3.models import GetObjectTextInput, PutObjectInput
from dex_connectors.amazon_s3.operations import _error_code, _metadata, _parse_media_type


def test_media_type_grammar() -> None:
    """Accept valid parameters and reject values Go's media parser rejects."""
    assert _parse_media_type('text/plain; charset="utf-8"') == "text/plain"
    assert _parse_media_type("text/plain; charset") is None
    assert _parse_media_type("text plain") is None
    assert _parse_media_type("text/plain; charset=utf-8; charset=latin1") is None


def test_repeated_metadata_headers_are_joined() -> None:
    """Preserve every value of a repeated user metadata header in wire order."""
    response = Response(
        200,
        httpx.Headers(
            [
                ("Content-Length", "3"),
                ("Last-Modified", "Sun, 25 Oct 2026 09:00:00 GMT"),
                ("X-Amz-Meta-Owner", "one"),
                ("X-Amz-Meta-Owner", "two"),
            ]
        ),
        b"",
    )
    metadata = _metadata(response, Session("access", "secret", ""), "acme-reports", "report.txt")
    assert metadata is not None
    assert metadata.last_modified == datetime(2026, 10, 25, 9, tzinfo=UTC)
    assert metadata.metadata == {"owner": "one,two"}


def test_error_code_requires_error_root() -> None:
    """Ignore a Code field outside S3's Error document."""
    session = Session("access", "secret", "")
    assert _error_code(Response(403, httpx.Headers(), b"<Other><Code>SlowDown</Code></Other>"), session) == ""
    assert (
        _error_code(
            Response(
                403,
                httpx.Headers(),
                b'<Error xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Code>SlowDown</Code></Error>',
            ),
            session,
        )
        == "SlowDown"
    )


async def test_invalid_media_type_never_stores_or_reads_text() -> None:
    """Reject a malformed PUT type before sending and a malformed stored type after GET."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200,
            headers={
                "Content-Type": "text/plain; charset",
                "Content-Length": "3",
                "Last-Modified": "Sun, 25 Oct 2026 09:00:00 GMT",
            },
            content=b"hey",
        )

    connection = ConnectionRef("amazon-s3", "s3-test")
    credentials = StaticCredentialProvider(
        {
            connection: Credentials(
                SecretString("AKIAS3CONNECTORTEST1"),
                SecretString("safe-secret"),
                SecretString(""),
            )
        }
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(endpoint="http://127.0.0.1:12345", default_bucket="acme-reports", region="eu-west-1"),
            credentials,
            http_client,
            lambda: datetime(2026, 10, 25, 9, tzinfo=UTC),
        )
        call = Call(
            CallID("5908a179-2147-5839-9c64-529cd36f77d7"),
            connection,
            OperationRef("amazon-s3", "putObject"),
            IdempotencyKey("stable-key"),
        )
        put = await client.put_object().invoke(
            call, PutObjectInput(key="report.txt", content_type="text/plain; charset")
        )
        assert put.branch == "defect"
        assert requests == []
        get = await client.get_object_text().invoke(call, GetObjectTextInput(key="report.txt"))
        assert get.branch == "unsupportedContent"
        assert len(requests) == 1
