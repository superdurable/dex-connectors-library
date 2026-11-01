# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Postmark outbound message Queries."""

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

from dex_connectors.postmark import (
    DEFECT,
    FOUND,
    GET_MESSAGE_DETAILS_DEFINITION,
    INVALID_RESPONSE,
    LIST_OUTBOUND_MESSAGES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetMessageDetailsInput,
    ListOutboundMessagesInput,
)

CONNECTION = ConnectionRef("postmark", "postmark")
TOKEN = "00000000-0000-0000-0000-000000000000"
MESSAGE_ID = "0ac29aee-e1cd-480d-b08d-4f48548ff48d"
MESSAGE = {"MessageID": MESSAGE_ID, "Subject": "Welcome", "Status": "Sent", "To": []}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=256),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_OUTBOUND_MESSAGES_DEFINITION.operation)


def details_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_MESSAGE_DETAILS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_list_defaults_count_and_sends_server_token_header() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"TotalCount": 1, "Messages": [MESSAGE]}), requests)
    result = await run_query(client.list_outbound_messages(), list_call(), ListOutboundMessagesInput())
    assert result.branch == LISTED
    assert result.value.total_count == 1
    assert result.value.messages[0].message_id == MESSAGE_ID
    request = requests[0]
    assert request.url.path == "/messages/outbound"
    assert dict(request.url.params) == {"count": "50", "offset": "0"}
    assert request.headers["X-Postmark-Server-Token"] == TOKEN
    assert "Authorization" not in request.headers


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "search",
    [
        ListOutboundMessagesInput(count=501),
        ListOutboundMessagesInput(count=-1),
        ListOutboundMessagesInput(offset=-1),
        ListOutboundMessagesInput(count=500, offset=9_501),
    ],
)
async def test_list_rejects_window_outside_postmark_limits(search: ListOutboundMessagesInput) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"TotalCount": 0, "Messages": []}), requests)
    result = await run_query(client.list_outbound_messages(), list_call(), search)
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(200, json={"TotalCount": 0}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (
            httpx.Response(200, json={"TotalCount": 0, "Messages": None}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, content=b"x" * 300), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
        (httpx.Response(404), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
    ],
)
async def test_list_terminal_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(
        make_client(response).list_outbound_messages(), list_call(), ListOutboundMessagesInput()
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(429, headers={"Retry-After": "2"}), FailureKind.RATE_LIMIT),
        (httpx.Response(500), FailureKind.AVAILABILITY),
        (httpx.Response(408), FailureKind.AVAILABILITY),
    ],
)
async def test_list_retries_safe_failures(response: httpx.Response, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(
            make_client(response).list_outbound_messages(), list_call(), ListOutboundMessagesInput()
        )
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_details_escapes_id_and_decodes_message() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json=MESSAGE), requests)
    result = await run_query(client.get_message_details(), details_call(), GetMessageDetailsInput(MESSAGE_ID))
    assert result.branch == FOUND
    assert result.value.status == "Sent"
    await run_query(client.get_message_details(), details_call(), GetMessageDetailsInput("a/b"))
    assert requests[1].url.raw_path == b"/messages/outbound/a%2Fb/details"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(422, headers={"X-PM-ApiErrorCode": "701"}), NOT_FOUND),
        (httpx.Response(422, json={"ErrorCode": 701, "Message": "missing"}), NOT_FOUND),
        (httpx.Response(422, json={"ErrorCode": 300}), PROVIDER_REJECTED),
        (httpx.Response(200, json={"Subject": "x"}), INVALID_RESPONSE),
    ],
)
async def test_details_branches(response: httpx.Response, branch: str) -> None:
    result = await run_query(
        make_client(response).get_message_details(), details_call(), GetMessageDetailsInput(MESSAGE_ID)
    )
    assert result.branch == branch


@pytest.mark.asyncio
async def test_blank_id_and_token_are_defects_without_request() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json=MESSAGE), requests)
    blank_id = await run_query(client.get_message_details(), details_call(), GetMessageDetailsInput(""))
    assert blank_id.branch == DEFECT
    blank_token = await run_query(
        make_client(httpx.Response(200, json=MESSAGE), requests, token=" ").get_message_details(),
        details_call(),
        GetMessageDetailsInput(MESSAGE_ID),
    )
    assert blank_token.branch == DEFECT
    assert blank_token.failure is not None and blank_token.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
