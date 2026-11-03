# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check local validation and secret-safe provider response handling."""

import json
from uuid import UUID

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

from dex_connectors.atlassian_trello import (
    AddCommentInput,
    Client,
    Config,
    CreateCardInput,
    Credentials,
    GetCardInput,
    ListCardsInput,
    UpdateCardInput,
    operations,
)
from dex_connectors.atlassian_trello.client import ProviderResponse

CONNECTION = ConnectionRef("trello", "trello-test")
CARD_ID = "6512f0a1c2d3e4f5a6b7c8d9"


def make_client(handler: httpx.MockTransport) -> Client:
    """Bind inert credentials to a local HTTP transport."""
    return Client(
        Config("http://127.0.0.1:12345/1"),
        StaticCredentialProvider(
            {CONNECTION: Credentials(SecretString("test-key"), SecretString("test-token"))}
        ),
        httpx.AsyncClient(transport=handler),
    )


def make_call(operation: str) -> Call:
    """Construct a stable operation Call without a Dex context for local checks."""
    return Call(
        CallID(str(UUID("fed5a116-c5f6-53ac-81b1-a823fb443a88"))),
        CONNECTION,
        OperationRef("trello", operation),
    )


@pytest.mark.parametrize(
    "operation,input_value",
    [
        ("listCards", ListCardsInput(board_id=CARD_ID, list_id=CARD_ID)),
        ("getCard", GetCardInput("../unsafe")),
        ("createCard", CreateCardInput(CARD_ID, "bad\nname")),
        ("updateCard", UpdateCardInput(CARD_ID)),
        ("addComment", AddCommentInput(CARD_ID, "\x01invalid")),
    ],
)
async def test_invalid_input_never_contacts_trello(operation: str, input_value: object) -> None:
    """Reject malformed input before credential lookup or provider dispatch."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200)

    client = make_client(httpx.MockTransport(answer))
    attempt = await getattr(
        client,
        {
            "listCards": "list_cards",
            "getCard": "get_card",
            "createCard": "create_card",
            "updateCard": "update_card",
            "addComment": "add_comment",
        }[operation],
    )().invoke(make_call(operation), input_value)
    assert attempt.branch == "defect"
    assert not requests
    await client.http_client.aclose()


async def test_reflected_credentials_select_invalid_response_without_leaking_secret() -> None:
    """A success body containing secret material never becomes a Flow result."""
    client = make_client(httpx.MockTransport(lambda _: httpx.Response(200, text='{"token":"test-token"}')))
    attempt = await client.get_card().invoke(make_call("getCard"), GetCardInput(CARD_ID))
    assert attempt.branch == "invalidResponse"
    assert attempt.failure is not None
    assert "test-token" not in attempt.failure.message
    assert "test-key" not in attempt.failure.message
    await client.http_client.aclose()


async def test_quote_breaking_token_is_rejected_before_http() -> None:
    """A token cannot escape Trello's quoted Authorization header."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200)

    client = Client(
        Config("http://127.0.0.1:12345/1"),
        StaticCredentialProvider(
            {CONNECTION: Credentials(SecretString("test-key"), SecretString('bad",oauth_token="other'))}
        ),
        httpx.AsyncClient(transport=httpx.MockTransport(answer)),
    )
    attempt = await client.get_card().invoke(make_call("getCard"), GetCardInput(CARD_ID))
    assert attempt.branch == "defect"
    assert not requests
    await client.http_client.aclose()


@pytest.mark.parametrize("position", ["1_0", "１２", "1e", "0x1", "1e9999", "-1", "NaN"])
def test_invalid_position_syntax_is_rejected(position: str) -> None:
    """Reject syntax Python float accepts but Trello's position contract does not."""
    with pytest.raises(ValueError, match="position must be"):
        operations._position(position)


@pytest.mark.parametrize(
    "position,expected",
    [("1e2", 100.0), ("0x1p2", 4.0), (".5", 0.5), ("top", "top")],
)
def test_valid_position_syntax(position: str, expected: str | float) -> None:
    """Keep decimal, hexadecimal, and named positions compatible with Go."""
    assert operations._position(position) == expected


@pytest.mark.parametrize(
    "board_id,short_link,expected_board_id,expected_short_link",
    [
        ("Z" * 24, "A!cD1234", "", ""),
        ("a" * 24, "AbCd1234", "a" * 24, "AbCd1234"),
    ],
)
async def test_created_card_references_match_go_patterns(
    monkeypatch: pytest.MonkeyPatch,
    board_id: str,
    short_link: str,
    expected_board_id: str,
    expected_short_link: str,
) -> None:
    """Omit malformed optional references from a successful create response."""
    async def dispatch(*_args: object) -> tuple[ProviderResponse, None]:
        response = ProviderResponse(
            200,
            httpx.Headers(),
            json.dumps({"id": CARD_ID, "idBoard": board_id, "shortLink": short_link}).encode(),
        )
        return response, None

    monkeypatch.setattr(operations, "_single_dispatch", dispatch)
    client = make_client(httpx.MockTransport(lambda _: httpx.Response(500)))
    attempt = await client.create_card().invoke(
        make_call("createCard"), CreateCardInput(CARD_ID, "Example")
    )
    assert attempt.branch == "created"
    assert attempt.value.board_id == expected_board_id
    assert attempt.value.short_link == expected_short_link
    await client.http_client.aclose()
