# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Figma file Queries."""

from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_query,
)

from dex_connectors.figma import (
    DEFECT,
    FOUND,
    GET_FILE_COMMENTS_DEFINITION,
    GET_FILE_DEFINITION,
    INVALID_RESPONSE,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetFileCommentsInput,
    GetFileInput,
)

CONNECTION = ConnectionRef("figma", "figma")
TOKEN = "figd_test_token"
DOCUMENT = {"id": "0:0", "type": "DOCUMENT", "name": "Root", "children": [{"id": "1:1", "type": "CANVAS"}]}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/v1", max_response_bytes=400),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def file_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_FILE_DEFINITION.operation)


def comments_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_FILE_COMMENTS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_get_file_keeps_document_tree_and_sends_figma_token() -> None:
    requests: list[httpx.Request] = []
    body = {"name": "Meridian Design", "lastModified": "2026-10-28T12:00:00Z", "document": DOCUMENT}
    result = await run_query(
        make_client(httpx.Response(200, json=body), requests).get_file(),
        file_call(),
        GetFileInput("AbC_123-x"),
    )
    assert result.branch == FOUND
    assert result.value.document == DOCUMENT
    assert result.value.last_modified == "2026-10-28T12:00:00Z"
    assert requests[0].url.raw_path == b"/v1/files/AbC_123-x"
    assert requests[0].headers["X-Figma-Token"] == TOKEN


@pytest.mark.asyncio
async def test_get_file_comments_lists_messages() -> None:
    requests: list[httpx.Request] = []
    body = {"comments": [{"id": "1", "message": "Review the header.", "user": {"handle": "x"}}]}
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(client.get_file_comments(), comments_call(), GetFileCommentsInput("AbC"))
    assert result.branch == LISTED
    assert result.value.comments[0].message == "Review the header."
    assert requests[0].url.raw_path == b"/v1/files/AbC/comments"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("body", "branch"),
    [
        ({"name": "x", "document": None}, INVALID_RESPONSE),
        ({"document": DOCUMENT}, INVALID_RESPONSE),
        ({"name": 1, "document": DOCUMENT}, INVALID_RESPONSE),
    ],
)
async def test_incomplete_files_are_invalid(body: object, branch: str) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).get_file(), file_call(), GetFileInput("AbC")
    )
    assert result.branch == branch


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.NOT_FOUND),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(400), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(200, json={"comments": None}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 500), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_comment_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(
        make_client(response).get_file_comments(), comments_call(), GetFileCommentsInput("AbC")
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 409, 429, 503])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_query(make_client(httpx.Response(status)).get_file(), file_call(), GetFileInput("AbC"))


@pytest.mark.asyncio
@pytest.mark.parametrize("file_key", ["", "a/b", "x" * 129])
async def test_invalid_file_key_is_defect_without_request(file_key: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).get_file(), file_call(), GetFileInput(file_key)
    )
    assert result.branch == DEFECT
    assert requests == []
