# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go net/url.QueryEscape and PathEscape reference vectors."""

from datetime import UTC, datetime

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

from dex_connectors.microsoft_onedrive import Client, Config, Credentials, GetFileInput
from dex_connectors.microsoft_onedrive.client import path_escape, query_escape
from dex_connectors.microsoft_onedrive.operations import GetFileOperation, _go_equal_fold, _go_path_extension


@pytest.mark.parametrize(
    ("source", "query", "path"),
    [
        ("a b~*!'()", "a+b~%2A%21%27%28%29", "a%20b~%2A%21%27%28%29"),
        ("a/b;c,d?e", "a%2Fb%3Bc%2Cd%3Fe", "a%2Fb%3Bc%2Cd%3Fe"),
        ("é &+%", "%C3%A9+%26%2B%25", "%C3%A9%20&+%25"),
        ("$select=id,name", "%24select%3Did%2Cname", "$select=id%2Cname"),
        ("O'Neil", "O%27Neil", "O%27Neil"),
    ],
)
def test_go_url_escape(source: str, query: str, path: str) -> None:
    """Preserve Go's exact escaped bytes, including literal tilde and path delimiters."""
    assert query_escape(source) == query
    assert path_escape(source) == path


def test_go_equal_fold_does_not_expand_sharp_s() -> None:
    assert not _go_equal_fold("Straße", "STRASSE")
    assert _go_equal_fold("Straße", "STRAẞE")


def test_go_path_extension_includes_leading_dot() -> None:
    assert _go_path_extension(".txt") == ".txt"
    assert _go_path_extension("folder/file.txt") == ".txt"


@pytest.mark.asyncio
async def test_submicrosecond_item_timestamp_is_invalid_response() -> None:
    connection = ConnectionRef("microsoft", "onedrive-files")
    call = Call(
        CallID("11111111-1111-5111-8111-111111111111"),
        connection,
        OperationRef("microsoft-onedrive", "getFile"),
    )
    credentials = Credentials(access_token=SecretString("synthetic-token"))
    body = b'{"id":"item-1","name":"file.txt","createdDateTime":"2026-01-28T13:00:05.123456789Z"}'
    transport = httpx.MockTransport(lambda _: httpx.Response(200, content=body))
    async with httpx.AsyncClient(transport=transport) as http_client:
        client = Client(
            Config(endpoint="http://127.0.0.1:12345"),
            StaticCredentialProvider({connection: credentials}),
            http_client,
            now=lambda: datetime(2026, 1, 1, tzinfo=UTC),
        )
        result = await GetFileOperation(client).invoke(call, GetFileInput(item_id="item-1"))
    assert result.kind == "branch" and result.branch == "invalidResponse"
    assert result.failure is not None and result.failure.kind.value == "PROTOCOL"
