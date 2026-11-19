# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""createContact never resends an accepted create and keeps its send count."""

import asyncio
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    CredentialProvider,
    MutationResult,
    OperationRef,
    ReauthorizationRequiredError,
    RetryError,
    SecretString,
    run_mutation,
)
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)

from dex_connectors.google_contacts import Client, Config, Contact, CreateContactInput, Credentials
from dex_connectors.google_contacts._generated.connection import CredentialMaterialError, decode_credentials
from dex_connectors.google_contacts._generated.definitions import CREATE_CONTACT_DEFINITION

REFERENCE = ConnectionRef("google-contacts", "google-contacts")
CALL_ID = CallID("74390522-858d-5ed6-bdf7-04e979ba9e4f")
TOKEN = "synthetic-contacts-token"
INPUT = CreateContactInput("Ada")


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, _value_type: object) -> object:
        """Return the last heartbeat as the SDK wrote it."""
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value

    def advance(self) -> None:
        """Start the next Execute attempt with a fresh deadline."""
        self.attempt += 1
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)


class FailingResolutionCredentials(CredentialProvider[Credentials]):
    """Fail one numbered credential resolution transiently."""

    def __init__(self, failed_resolution: int = 0, error: Exception | None = None) -> None:
        self.failed_resolution = failed_resolution
        self.error = error or OSError("credential refresh unavailable")
        self.resolutions = 0

    async def resolve(self, call: Call) -> Credentials:
        """Resolve a static token, except on the failed resolution."""
        self.resolutions += 1
        if self.resolutions == self.failed_resolution:
            raise self.error
        return Credentials("client-id", SecretString("secret"), SecretString(TOKEN), SecretString("refresh"))


class BrokenStream(httpx.AsyncByteStream):
    """A response body that breaks after the 2xx headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        raise httpx.ReadError("interrupted response")
        yield b""  # pragma: no cover


Answer = Callable[[httpx.Request], httpx.Response]


async def run_attempts(
    answer: Answer,
    requests: list[httpx.Request],
    attempts: int,
    failed_resolution: int = 0,
    error: Exception | None = None,
) -> MutationResult[Contact] | RetryError:
    """Run attempts that must Retry, then return the final attempt's outcome."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return answer(request)

    context = AttemptContext()
    credentials = FailingResolutionCredentials(failed_resolution, error)
    outcome: MutationResult[Contact] | RetryError | None = None
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(base_url="http://127.0.0.1:12345"), credentials, transport)
        for attempt in range(1, attempts + 1):
            if attempt > 1:
                assert isinstance(outcome, RetryError), f"attempt {attempt - 1}: {outcome}"
                context.advance()
            call = Call(
                CALL_ID,
                REFERENCE,
                OperationRef("google-contacts", "createContact"),
                context=context,
                max_sends=CREATE_CONTACT_DEFINITION.max_sends,
            )
            try:
                outcome = await run_mutation(client.create_contact(), call, INPUT)
            except RetryError as error:
                outcome = error
    assert outcome is not None
    return outcome


def posts(requests: list[httpx.Request]) -> int:
    """Count createContact sends."""
    return sum(request.method == "POST" for request in requests)


async def test_accepted_create_without_a_resource_name_is_never_resent() -> None:
    """A 2xx proves the create even when its body is unusable or breaks."""
    answers: list[Answer] = [
        lambda _request: httpx.Response(200, content=b'{"resourceName":'),
        lambda _request: httpx.Response(200, json={"names": []}),
        lambda _request: httpx.Response(202),
        lambda _request: httpx.Response(200, stream=BrokenStream()),
    ]
    for answer in answers:
        requests: list[httpx.Request] = []
        outcome = await run_attempts(answer, requests, 1)
        assert isinstance(outcome, MutationResult)
        assert outcome.branch == "uncertain"
        assert posts(requests) == 1


async def test_resource_name_with_an_unknown_status_is_read_back() -> None:
    """A resource name proves the create, so the next attempt reads it back instead of resending."""

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            person = {"resourceName": "people/c125", "names": [{"displayName": "Ada"}]}
            return httpx.Response(200, json=person)
        return httpx.Response(409, json={"resourceName": "people/c125"})

    requests: list[httpx.Request] = []
    outcome = await run_attempts(answer, requests, 2)
    assert isinstance(outcome, MutationResult)
    assert outcome.branch == "created"
    assert outcome.value.resource_name == "people/c125"
    assert posts(requests) == 1


async def test_read_back_whose_body_breaks_is_retried() -> None:
    """A saved contact is read again after a broken read-back body, never resent."""
    reads = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal reads
        if request.method == "POST":
            return httpx.Response(409, json={"resourceName": "people/c126"})
        reads += 1
        if reads == 1:
            return httpx.Response(200, stream=BrokenStream())
        return httpx.Response(200, json={"resourceName": "people/c126", "names": [{"displayName": "Ada"}]})

    requests: list[httpx.Request] = []
    outcome = await run_attempts(answer, requests, 3)
    assert isinstance(outcome, MutationResult)
    assert outcome.branch == "created"
    assert posts(requests) == 1
    assert reads == 2


class StalledStream(httpx.AsyncByteStream):
    """A response body that stalls after the 2xx headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(5)
        yield b"{}"


async def test_write_deadline_that_cuts_an_accepted_body_is_never_resent() -> None:
    """2xx headers arrived, so the Fence write-window timeout proves acceptance, not an unknown outcome."""
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, stream=StalledStream())

    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(base_url="http://127.0.0.1:12345"), FailingResolutionCredentials(), transport)
        call = Call(
            CALL_ID,
            REFERENCE,
            OperationRef("google-contacts", "createContact"),
            context=context,
            max_sends=CREATE_CONTACT_DEFINITION.max_sends,
        )
        outcome = await run_mutation(client.create_contact(), call, INPUT)
    assert isinstance(outcome, MutationResult)
    assert outcome.branch == "uncertain"
    assert posts(requests) == 1


async def test_unreadable_refusal_is_an_unknown_outcome() -> None:
    """A refusal whose body cannot be read safely might hide a created contact."""
    requests: list[httpx.Request] = []
    outcome = await run_attempts(lambda _request: httpx.Response(400, json={"error": TOKEN}), requests, 1)
    assert isinstance(outcome, RetryError)
    assert posts(requests) == 1


async def test_pre_send_retry_after_a_possible_send_keeps_the_send_count() -> None:
    """Attempt 1 is unknown, attempt 2 gets 429, attempt 3 fails credentials, attempt 4 is at the cap."""
    statuses = iter([500, 429, 500])
    requests: list[httpx.Request] = []
    outcome = await run_attempts(lambda _request: httpx.Response(next(statuses)), requests, 4, 3)
    assert isinstance(outcome, MutationResult)
    assert outcome.branch == "uncertain"
    assert posts(requests) == 3


def sdk_wrapped_material_error() -> ValueError:
    """Build the ValueError the SDK project provider raises after the generated decoder refuses material."""
    try:
        try:
            decode_credentials({"unknown": "value"})
        except CredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("malformed material decoded")


CREDENTIAL_FAILURES: list[tuple[Exception, bool]] = [
    (sdk_wrapped_material_error(), True),
    (ReauthorizationRequiredError(), True),
    (ObjectNotFoundError(), True),
    (ConnectionNotSelectedError(), True),
    (CredentialBrokerRejectedError(), True),
    (CredentialBrokerUnavailableError(), False),
    (ValueError("connection is not configured"), False),
    (RuntimeError("unlisted"), False),
]


@pytest.mark.parametrize(("error", "is_terminal"), CREDENTIAL_FAILURES)
async def test_credential_failure_on_a_first_send(error: Exception, is_terminal: bool) -> None:
    """Only a reconnection-only failure selects defect; nothing is sent either way."""
    requests: list[httpx.Request] = []
    outcome = await run_attempts(lambda _request: httpx.Response(500), requests, 1, 1, error)
    if is_terminal:
        assert isinstance(outcome, MutationResult) and outcome.branch == "defect"
    else:
        assert isinstance(outcome, RetryError)
    assert posts(requests) == 0


@pytest.mark.parametrize(("error", "is_terminal"), CREDENTIAL_FAILURES)
async def test_credential_failure_after_a_possible_send(error: Exception, is_terminal: bool) -> None:
    """After an unknown send, a terminal failure selects uncertain and a transient one Retries."""
    requests: list[httpx.Request] = []
    outcome = await run_attempts(lambda _request: httpx.Response(500), requests, 2, 2, error)
    if is_terminal:
        assert isinstance(outcome, MutationResult) and outcome.branch == "uncertain"
    else:
        assert isinstance(outcome, RetryError)
    assert posts(requests) == 1
