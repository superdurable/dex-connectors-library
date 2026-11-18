# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of upsertRow appends whose outcome is unknown."""

import asyncio
import json
from collections.abc import AsyncIterator, Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    CredentialProvider,
    MutationResult,
    OperationRef,
    RetryError,
    SecretString,
    run_mutation,
)

from dex_connectors.google_spreadsheet import Client, Config, Credentials
from dex_connectors.google_spreadsheet._generated.definitions import UPSERT_ROW_DEFINITION
from dex_connectors.google_spreadsheet.models import UpsertRowInput, UpsertRowOutput

REFERENCE = ConnectionRef("google-sheets", "workspace")
CALL_ID = CallID("74390522-858d-5ed6-bdf7-04e979ba9e4f")
HEADER = ["accountId", "name"]
ADA = ["a-1", "Ada"]
INPUT = UpsertRowInput("sheet", "Customers", "accountId", "a-1", {"accountId": "a-1", "name": "Ada"})


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

    def __init__(self, failed_resolution: int = 0) -> None:
        self.failed_resolution = failed_resolution
        self.resolutions = 0

    async def resolve(self, call: Call) -> Credentials:
        """Resolve a static token, except on the failed resolution."""
        self.resolutions += 1
        if self.resolutions == self.failed_resolution:
            raise RuntimeError("credential refresh unavailable")
        empty = SecretString("")
        return Credentials("", empty, SecretString("sheets-token"), empty)


class BrokenStream(httpx.AsyncByteStream):
    """A response body that breaks after the 2xx headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        raise httpx.ReadError("interrupted response")
        yield b""  # pragma: no cover


AppendAnswer = Callable[["ScriptedSheet"], httpx.Response]


def append_status(status: int, *added_rows: list[str]) -> AppendAnswer:
    """Add rows to the sheet, then answer the append with a bare status."""

    def answer(sheet: ScriptedSheet) -> httpx.Response:
        sheet.rows.extend(added_rows)
        return httpx.Response(status)

    return answer


@dataclass
class ScriptedSheet:
    """Serve rows to each GET unless a lookup status is queued; script each POST."""

    appends: list[AppendAnswer]
    lookup_statuses: list[int] = field(default_factory=list)
    rows: list[list[str]] = field(default_factory=lambda: [list(HEADER)])
    posts: int = 0

    def respond(self, request: httpx.Request) -> httpx.Response:
        """Answer one provider request."""
        if request.method == "GET":
            status = self.lookup_statuses.pop(0) if self.lookup_statuses else 0
            if status:
                return httpx.Response(status)
            return httpx.Response(200, json={"range": "Customers", "values": self.rows})
        assert request.method == "POST"
        self.posts += 1
        assert self.posts <= len(self.appends), "unexpected append"
        return self.appends[self.posts - 1](self)


async def upsert(
    sheet: ScriptedSheet, context: AttemptContext, credentials: CredentialProvider[Credentials] | None = None
) -> MutationResult[UpsertRowOutput] | RetryError:
    """Run one Execute attempt of upsertRow with the definition's send cap."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(sheet.respond)) as transport:
        provider = credentials or FailingResolutionCredentials()
        client = Client(Config(endpoint="http://localhost"), provider, transport)
        call = Call(
            CALL_ID,
            REFERENCE,
            OperationRef("google-sheets", "upsertRow"),
            context=context,
            max_sends=UPSERT_ROW_DEFINITION.max_sends,
        )
        try:
            return await run_mutation(client.upsert_row(), call, INPUT)
        except RetryError as error:
            return error


async def run_attempts(
    sheet: ScriptedSheet, attempts: int, credentials: CredentialProvider[Credentials] | None = None
) -> MutationResult[UpsertRowOutput]:
    """Run attempts that must Retry, then return the final attempt's result."""
    context = AttemptContext()
    for attempt in range(1, attempts):
        retried = await upsert(sheet, context, credentials)
        assert isinstance(retried, RetryError), f"attempt {attempt}: {retried}"
        context.advance()
    result = await upsert(sheet, context, credentials)
    assert isinstance(result, MutationResult), result
    return result


async def test_duplicate_key_rows_in_the_sending_attempt_read_back_are_uncertain() -> None:
    """The append of this attempt may be one of the duplicate rows."""
    sheet = ScriptedSheet([append_status(500, ADA, ADA)])
    result = await run_attempts(sheet, 1)
    assert result.branch == "uncertain"
    assert sheet.posts == 1


async def test_duplicate_key_rows_in_a_later_lookup_are_uncertain() -> None:
    """A later attempt never selects conflict after a possible append."""
    sheet = ScriptedSheet([append_status(500, ADA, ADA)], lookup_statuses=[0, 503])
    result = await run_attempts(sheet, 2)
    assert result.branch == "uncertain"
    assert sheet.posts == 1


async def test_failed_lookup_after_a_possible_append_retries_until_the_send_cap() -> None:
    """A rejected lookup retries below the cap and selects uncertain at it."""
    sheet = ScriptedSheet(
        [append_status(500), append_status(500)],
        # Lookup, read-back, rejected lookup, lookup, unavailable read-back, rejected lookup at the cap.
        lookup_statuses=[0, 0, 403, 0, 503, 403],
    )
    result = await run_attempts(sheet, 4)
    assert result.branch == "uncertain"
    assert sheet.posts == 2


async def test_reads_back_after_a_rate_limited_resend() -> None:
    """Attempt 1 is unknown, attempt 2's resend gets 429, attempt 3 reads back first."""
    sheet = ScriptedSheet([append_status(500), append_status(429, ADA)])
    result = await run_attempts(sheet, 3)
    assert result.branch == "upserted"
    assert result.value.action == "inserted"
    assert sheet.posts == 2


async def test_pre_send_retry_after_a_rate_limited_resend_keeps_the_send_count() -> None:
    """A transient credential failure after a possible send never records notSent."""
    sheet = ScriptedSheet([append_status(500), append_status(429, ADA)])
    result = await run_attempts(sheet, 4, FailingResolutionCredentials(failed_resolution=3))
    assert result.branch == "upserted"
    assert result.value.action == "inserted"
    assert sheet.posts == 2


async def test_accepted_append_is_never_resent_when_its_row_is_missing() -> None:
    """A 2xx or a body naming the appended range proves the append, so it is never resent."""
    answers: list[tuple[AppendAnswer, list[int], int]] = [
        (lambda _sheet: httpx.Response(200, content=b'{"updates":'), [], 1),
        (
            lambda _sheet: httpx.Response(
                400, content=json.dumps({"updates": {"updatedRange": "'Customers'!A2:B2"}}).encode()
            ),
            [],
            1,
        ),
        (lambda _sheet: httpx.Response(200, json={"updates": {}}), [0, 503], 2),
        (lambda _sheet: httpx.Response(200, stream=BrokenStream()), [], 1),
    ]
    for answer, lookup_statuses, attempts in answers:
        sheet = ScriptedSheet([answer], lookup_statuses=list(lookup_statuses))
        result = await run_attempts(sheet, attempts)
        assert result.branch == "uncertain"
        assert sheet.posts == 1


class StalledStream(httpx.AsyncByteStream):
    """A response body that stalls after the 2xx headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(5)
        yield b"{}"


async def test_write_deadline_that_cuts_an_accepted_body_is_never_resent() -> None:
    """2xx headers arrived, so the Fence write-window timeout proves the append, not an unknown outcome."""
    sheet = ScriptedSheet([lambda _sheet: httpx.Response(200, stream=StalledStream())])
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    result = await upsert(sheet, context)
    assert isinstance(result, MutationResult), result
    assert result.branch == "uncertain"
    assert sheet.posts == 1


async def test_unreadable_append_refusal_is_an_unknown_outcome() -> None:
    """A refusal is definite only when its whole body was read."""
    answers: list[AppendAnswer] = [
        lambda _sheet: httpx.Response(400, content=b"x" * (Config().max_response_bytes + 1)),
        lambda _sheet: httpx.Response(400, stream=BrokenStream()),
    ]
    for answer in answers:
        sheet = ScriptedSheet([answer])
        context = AttemptContext()
        assert isinstance(await upsert(sheet, context), RetryError)
        assert sheet.posts == 1


async def test_interrupted_success_body_with_a_visible_row_is_upserted() -> None:
    """A 2xx whose body breaks is read back and never resent."""

    def answer(sheet: ScriptedSheet) -> httpx.Response:
        sheet.rows.append(ADA)
        return httpx.Response(200, stream=BrokenStream())

    sheet = ScriptedSheet([answer])
    result = await run_attempts(sheet, 1)
    assert result.branch == "upserted"
    assert result.value.action == "inserted"
    assert sheet.posts == 1
