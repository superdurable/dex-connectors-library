# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Pipedrive creates and retry of repeat-safe updates."""

from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from typing import Any, cast

import httpx
import pytest
from dex import AsyncContext
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    Mutation,
    MutationResult,
    OperationRef,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)
from dex_connectors.sdk.singledispatch import Marker, State

from dex_connectors.pipedrive import (
    Client,
    Config,
    CreateObjectInput,
    Credentials,
    UpdateObjectInput,
    UpsertObjectInput,
)
from dex_connectors.pipedrive._generated.definitions import (
    CREATE_OBJECT_DEFINITION,
    UPSERT_OBJECT_DEFINITION,
)

CONNECTION = ConnectionRef("pipedrive", "pipedrive-crm")
CALL_ID = CallID("caa4781a-357f-5359-9562-1241cba08988")
EMPTY_SEARCH = {"success": True, "data": {"items": []}, "additional_data": {"next_cursor": None}}
PERSON = {
    "success": True,
    "data": {
        "id": 901,
        "name": "Jane Smith",
        "emails": [{"value": "jane@acme.example.com", "primary": True}],
    },
}


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's marker from a single-marker or per-call map heartbeat."""
        if isinstance(self.heartbeat_value, Marker):
            return self.heartbeat_value
        if not isinstance(self.heartbeat_value, dict):
            return None
        entry = self.heartbeat_value.get("singleDispatchMarkers", {}).get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the last heartbeat, translating a seeded marker to the requested format."""
        if isinstance(self.heartbeat_value, Marker) and value_type is not Marker:
            marker = self.heartbeat_value
            return {"singleDispatchMarkers": {str(marker.call_id): marker.to_wire()}}
        if isinstance(self.heartbeat_value, dict) and value_type is Marker:
            return self.marker
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


Responder = Callable[[httpx.Request], httpx.Response]


async def run(
    operation: str, input: Any, respond: Responder, context: AttemptContext, max_sends: int = 0
) -> MutationResult[Any] | RetryError:
    """Run one Execute attempt of a Pipedrive Mutation."""
    credentials = StaticCredentialProvider(
        {CONNECTION: Credentials("api-token", SecretString("inert-token"))}
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(endpoint="http://127.0.0.1:42702"), credentials, transport)
        factories: dict[str, Callable[[], Mutation[Any, Any]]] = {
            "createObject": client.create_object,
            "upsertObject": client.upsert_object,
            "updateObject": client.update_object,
        }
        call = Call(
            CALL_ID,
            CONNECTION,
            OperationRef("pipedrive", operation),
            context=cast(AsyncContext, context),
            max_sends=max_sends,
        )
        try:
            return await run_mutation(factories[operation](), call, input)
        except RetryError as error:
            return error


def deal() -> CreateObjectInput:
    """Return a deal create input."""
    return CreateObjectInput("deals", {"title": "Acme renewal"})


def person() -> UpsertObjectInput:
    """Return a person upsert input."""
    return UpsertObjectInput("persons", "email", "jane@acme.example.com", {"name": "Jane Smith"})


def answer(status: int, requests: list[httpx.Request]) -> Responder:
    """Answer every request with one status."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, json={"success": False})

    return respond


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [500, 409, 402, 302, 418])
async def test_create_resends_an_unknown_outcome_until_the_send_cap(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in (1, 2):
        context.attempt = attempt
        result = await run(
            "createObject", deal(), answer(status, requests), context, CREATE_OBJECT_DEFINITION.max_sends
        )
        assert isinstance(result, RetryError), f"attempt {attempt} is below the send cap"
    context.attempt = 3
    result = await run(
        "createObject", deal(), answer(status, requests), context, CREATE_OBJECT_DEFINITION.max_sends
    )
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 3


@pytest.mark.asyncio
async def test_create_records_can_resend_before_a_definite_first_send_refusal() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await run(
        "createObject", deal(), answer(400, requests), context, CREATE_OBJECT_DEFINITION.max_sends
    )
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"


@pytest.mark.asyncio
async def test_create_selects_uncertain_for_a_refusal_after_a_possible_send() -> None:
    requests: list[httpx.Request] = []
    statuses = iter([502, 400])

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(next(statuses), json={"success": False})

    context = AttemptContext()
    assert isinstance(
        await run("createObject", deal(), respond, context, CREATE_OBJECT_DEFINITION.max_sends), RetryError
    )
    context.attempt = 2
    result = await run("createObject", deal(), respond, context, CREATE_OBJECT_DEFINITION.max_sends)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_create_never_resends_an_accepted_but_unreadable_create() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(201, json={"success": True, "data": {"id": "x"}})

    result = await run("createObject", deal(), respond, AttemptContext(), CREATE_OBJECT_DEFINITION.max_sends)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("prior_attempt", "branch", "methods"),
    [
        (1, "upserted", ["GET", "POST"]),
        (3, "uncertain", ["GET"]),
    ],
)
async def test_upsert_searches_then_creates_again_only_below_the_send_cap(
    prior_attempt: int, branch: str, methods: list[str]
) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200 if request.method == "GET" else 201, json=EMPTY_SEARCH if request.method == "GET" else PERSON
        )

    context = AttemptContext()
    context.heartbeat_value = Marker(CALL_ID, prior_attempt, State.CLAIMED, send_count=prior_attempt)
    context.attempt = prior_attempt + 1
    result = await run("upsertObject", person(), respond, context, UPSERT_OBJECT_DEFINITION.max_sends)
    assert isinstance(result, MutationResult) and result.branch == branch
    assert [request.method for request in requests] == methods


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [402, 302, 418])
async def test_update_retries_a_status_that_does_not_prove_a_refusal(status: int) -> None:
    result = await run(
        "updateObject",
        UpdateObjectInput("deals", "42", {"stage_id": 3}),
        answer(status, []),
        AttemptContext(),
    )
    assert isinstance(result, RetryError)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"success": True, "data": {"id": 43, "title": "Other"}},
        {"success": True, "data": "unusable"},
        {"success": False},
    ],
)
async def test_update_retries_an_accepted_update_with_an_unusable_body(body: object) -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=body)

    result = await run(
        "updateObject", UpdateObjectInput("deals", "42", {"stage_id": 3}), respond, AttemptContext()
    )
    assert isinstance(result, RetryError)


@pytest.mark.asyncio
async def test_upsert_selects_uncertain_for_several_matches_after_a_possible_create() -> None:
    requests: list[httpx.Request] = []
    search = {
        "success": True,
        "data": {
            "items": [
                {"item": {"id": 901, "name": "Jane", "emails": ["jane@acme.example.com"]}},
                {"item": {"id": 902, "name": "Jane S", "emails": ["jane@acme.example.com"]}},
            ]
        },
        "additional_data": {"next_cursor": None},
    }

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=search)

    context = AttemptContext()
    context.heartbeat_value = Marker(CALL_ID, 1, State.CLAIMED, send_count=1)
    context.attempt = 2
    result = await run("upsertObject", person(), respond, context, UPSERT_OBJECT_DEFINITION.max_sends)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests] == ["GET"]
    context.heartbeat_value = None
    context.attempt = 1
    result = await run("upsertObject", person(), respond, context, UPSERT_OBJECT_DEFINITION.max_sends)
    assert isinstance(result, MutationResult) and result.branch == "multipleMatches"
