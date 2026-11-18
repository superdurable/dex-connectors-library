# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Mailchimp campaign sends whose outcome is unknown."""

import asyncio
from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.intuit_mailchimp as mailchimp
from dex_connectors.intuit_mailchimp._generated.definitions import SEND_CAMPAIGN_DEFINITION
from dex_connectors.intuit_mailchimp.operations import _LegacySendDispatchMarker

CONNECTION = sdk.ConnectionRef("mailchimp", "test")
CALL_ID = sdk.CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf")
CAMPAIGN_ID = "42694e9e57"
LIST_ID = "57afe96172"

Answer = Callable[[httpx.Request], httpx.Response]


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=60)
        self.value: object | None = None
        self.read_error: Exception | None = None
        self.heartbeats = 0

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.value is not None or self.read_error is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Decode the last value as the requested type, whatever checkpoint format the SDK wrote."""
        if self.read_error is not None:
            raise self.read_error
        if value_type is Marker:
            return self.marker
        if value_type is _LegacySendDispatchMarker:
            if not isinstance(self.value, _LegacySendDispatchMarker):
                raise TypeError("heartbeat has another shape")
            return self.value
        if isinstance(self.value, Marker):
            return {"singleDispatchMarkers": {str(self.value.call_id): self.value.to_wire()}}
        return self.value

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's heartbeat exactly as written."""
        self.heartbeats += 1
        self.value = value

    @property
    def marker(self) -> Marker:
        """Decode this call's marker from a single marker or the checkpoint map."""
        if isinstance(self.value, Marker):
            return self.value
        if not isinstance(self.value, dict):
            raise TypeError("heartbeat has another shape")
        return Marker.from_wire(self.value["singleDispatchMarkers"][str(CALL_ID)])


def campaign(status: str) -> httpx.Response:
    """Answer a campaign read with a campaign in status."""
    return httpx.Response(
        200,
        json={
            "id": CAMPAIGN_ID,
            "web_id": 42,
            "type": "regular",
            "status": status,
            "emails_sent": 0,
            "create_time": "2026-10-25T10:00:00Z",
            "settings": {"title": "Spring Launch", "subject_line": "Hello"},
            "recipients": {"list_id": LIST_ID, "list_name": "Audience", "recipient_count": 100},
        },
    )


def problem(status: int, title: str) -> httpx.Response:
    """Answer with a Mailchimp problem document."""
    return httpx.Response(status, json={"title": title})


class CampaignScript:
    """Answer reads with the current status and each send with its next scripted reply."""

    def __init__(self, status: str, *send_replies: Callable[[], httpx.Response]) -> None:
        self.status = status
        self.send_replies = list(send_replies)
        self.sends = 0
        self.requests: list[httpx.Request] = []

    def __call__(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        if request.method == "GET":
            return campaign(self.status)
        self.sends += 1
        if self.sends <= len(self.send_replies):
            return self.send_replies[self.sends - 1]()
        self.status = "sending"
        return httpx.Response(204)


def server_error() -> httpx.Response:
    """Answer a send with a 500."""
    return problem(500, "Internal Server Error")


class FailingCredentials:
    """Fail every credential lookup with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> mailchimp.Credentials:
        """Raise the configured lookup error."""
        raise self.error


async def send(
    answer: Answer,
    context: AttemptContext,
    *,
    max_sends: int = SEND_CAMPAIGN_DEFINITION.max_sends,
    credentials: object | None = None,
) -> sdk.MutationResult[mailchimp.SendCampaignOutput] | sdk.RetryError:
    """Run one Execute attempt of sendCampaign, as MutationStep passes the definition cap."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = mailchimp.Client(
            mailchimp.Config(),
            credentials  # type: ignore[arg-type]
            or sdk.StaticCredentialProvider(
                {CONNECTION: mailchimp.Credentials(sdk.SecretString("mailchimpfixturekey-us6"))}
            ),
            http_client,
        )
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("mailchimp", "sendCampaign"),
            context=context,  # type: ignore[arg-type]
            max_sends=max_sends,
        )
        try:
            return await sdk.run_mutation(
                client.send_campaign(), call, mailchimp.SendCampaignInput(CAMPAIGN_ID, LIST_ID)
            )
        except sdk.RetryError as error:
            return error


def branch(result: sdk.MutationResult[mailchimp.SendCampaignOutput] | sdk.RetryError) -> str:
    """Return the selected branch of a terminal result."""
    assert isinstance(result, sdk.MutationResult), result
    return result.branch


@pytest.mark.asyncio
async def test_unknown_send_outcome_is_resent_after_reading_a_draft() -> None:
    """A 500 after the send is retried, and the retry reads a draft and resends below the cap."""
    script = CampaignScript("save", server_error)
    context = AttemptContext()
    assert isinstance(await send(script, context), sdk.RetryError)
    assert context.marker.state == "claimed" and context.marker.send_count == 1
    context.attempt = 2
    assert branch(await send(script, context)) == "sent"
    assert script.sends == 2
    assert context.marker.send_count == 2


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_retryable"),
    [
        (RuntimeError("credential store unavailable"), True),
        (CredentialBrokerUnavailableError(), True),
        (ValueError("connection is not configured"), True),
        (sdk.ReauthorizationRequiredError("revoked"), False),
        (ObjectNotFoundError(), False),
        (ConnectionNotSelectedError(), False),
        (CredentialBrokerRejectedError(), False),
    ],
    ids=[
        "transient lookup",
        "broker unavailable",
        "not configured",
        "reauthorization required",
        "object not found",
        "not selected",
        "broker rejected",
    ],
)
async def test_credential_failure_after_a_possible_send(error: Exception, is_retryable: bool) -> None:
    """A transient lookup retries and a terminal one selects uncertain; neither records notSent."""
    script = CampaignScript("save", server_error)
    context = AttemptContext()
    assert isinstance(await send(script, context), sdk.RetryError)
    heartbeats = context.heartbeats
    context.attempt = 2
    result = await send(script, context, credentials=FailingCredentials(error))
    if is_retryable:
        assert isinstance(result, sdk.RetryError)
    else:
        assert branch(result) == "uncertain"
    assert context.heartbeats == heartbeats, "notSent would hide the earlier possible send"
    assert script.sends == 1


@pytest.mark.asyncio
async def test_read_back_confirms_an_earlier_send_without_resending() -> None:
    """A campaign read as sending after an unknown outcome selects alreadySent."""
    script = CampaignScript("save", server_error)
    context = AttemptContext()
    assert isinstance(await send(script, context), sdk.RetryError)
    script.status = "sending"
    context.attempt = 2
    assert branch(await send(script, context)) == "alreadySent"
    assert script.sends == 1


@pytest.mark.asyncio
async def test_uncertain_is_selected_only_at_the_send_cap() -> None:
    """Sends stop at maxSends, and an attempt at the cap only reads."""
    script = CampaignScript("save", server_error, server_error, server_error)
    context = AttemptContext()
    for attempt in range(1, SEND_CAMPAIGN_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await send(script, context), sdk.RetryError)
    context.attempt = SEND_CAMPAIGN_DEFINITION.max_sends
    assert branch(await send(script, context)) == "uncertain"
    assert script.sends == SEND_CAMPAIGN_DEFINITION.max_sends
    context.attempt += 1
    assert branch(await send(script, context)) == "uncertain"
    assert script.sends == SEND_CAMPAIGN_DEFINITION.max_sends
    single = CampaignScript("save", server_error)
    assert branch(await send(single, AttemptContext(), max_sends=1)) == "uncertain"


@pytest.mark.asyncio
async def test_definite_refusal_of_a_first_send_records_can_resend() -> None:
    """A 400 refusal of the first send selects providerRejected and is never resent."""
    script = CampaignScript("save", lambda: problem(400, "Bad Request"))
    context = AttemptContext()
    assert branch(await send(script, context)) == "providerRejected"
    assert context.marker.state == "canResend"
    assert script.sends == 1


@pytest.mark.asyncio
async def test_refused_resend_reads_the_campaign_instead_of_selecting_the_refusal() -> None:
    """A refusal after a possible earlier send selects uncertain or alreadySent."""
    script = CampaignScript("save", server_error, lambda: problem(400, "Bad Request"))
    context = AttemptContext()
    assert isinstance(await send(script, context), sdk.RetryError)
    context.attempt = 2
    assert branch(await send(script, context)) == "uncertain"
    assert len(script.requests) == 5


@pytest.mark.asyncio
async def test_unprocessed_send_is_retried_without_counting_it() -> None:
    """A 429 or 503 records canResend with the earlier count."""
    for reply in (lambda: problem(429, "Too Many Requests"), lambda: problem(503, "Service Unavailable")):
        script = CampaignScript("save", reply)
        context = AttemptContext()
        assert isinstance(await send(script, context), sdk.RetryError)
        assert context.marker.state == "canResend" and context.marker.send_count == 0
        context.attempt = 2
        assert branch(await send(script, context)) == "sent"
        assert context.marker.send_count == 1


class _BrokenStream(httpx.AsyncByteStream):
    async def __aiter__(self):  # type: ignore[override]
        yield b'{"partial":'
        raise httpx.ReadError("body interrupted")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "accepted",
    [
        lambda: httpx.Response(200, json={"echo": "mailchimpfixturekey-us6"}),
        lambda: httpx.Response(200, stream=_BrokenStream()),
    ],
    ids=["reflected key", "body broken after 2xx headers"],
)
async def test_accepted_2xx_with_an_unusable_body_is_sent_and_never_resent(
    accepted: Callable[[], httpx.Response],
) -> None:
    """A 2xx accepted the send whatever its body holds."""
    script = CampaignScript("save", accepted)
    result = await send(script, AttemptContext())
    assert branch(result) == "sent"
    assert script.sends == 1


@pytest.mark.asyncio
async def test_read_failure_after_a_throttled_resend_keeps_the_earlier_send_count() -> None:
    """With a possible earlier send, a pre-send Retry records no notSent that could hide it."""
    answers = iter(
        (campaign("save"), problem(500, "Internal Server Error"), campaign("save"),
         problem(429, "Too Many Requests"), problem(503, "Service Unavailable"))
    )
    context = AttemptContext()
    for attempt in (1, 2, 3):
        context.attempt = attempt
        assert isinstance(await send(lambda _request: next(answers), context), sdk.RetryError)
    assert context.marker.state == "canResend" and context.marker.send_count == 1


@pytest.mark.asyncio
async def test_unknown_statuses_after_a_send_are_retried() -> None:
    """409, 425, and redirects do not prove a refusal."""
    for status in (409, 425, 307):
        script = CampaignScript("save", lambda status=status: problem(status, "Unknown Outcome"))
        assert isinstance(await send(script, AttemptContext()), sdk.RetryError), status


@pytest.mark.asyncio
async def test_unreadable_checkpoint_is_retried_without_a_request() -> None:
    """A heartbeat read error fails closed and returns Retry, never a terminal branch."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return campaign("save")

    context = AttemptContext()
    context.read_error = RuntimeError("heartbeat store unavailable")
    context.attempt = 2
    result = await send(answer, context)
    assert isinstance(result, sdk.RetryError)
    assert requests == []


@pytest.mark.asyncio
async def test_legacy_checkpoint_never_sends_again() -> None:
    """A pre-fence checkpoint only reads the campaign."""
    for status, expected in (("sending", "alreadySent"), ("save", "uncertain")):
        script = CampaignScript(status)
        context = AttemptContext()
        context.value = _LegacySendDispatchMarker(CALL_ID)
        context.attempt = 2
        assert branch(await send(script, context)) == expected
        assert script.sends == 0
        assert context.heartbeats == 0


@pytest.mark.asyncio
async def test_safe_writes_retry_unknown_statuses() -> None:
    """upsertMember retries 409 and 425, which do not prove a refusal."""
    for reply in (problem(409, "Conflict"), problem(425, "Too Early")):
        transport = httpx.MockTransport(lambda _request, reply=reply: reply)
        async with httpx.AsyncClient(transport=transport) as http:
            client = mailchimp.Client(
                mailchimp.Config(),
                sdk.StaticCredentialProvider(
                    {CONNECTION: mailchimp.Credentials(sdk.SecretString("mailchimpfixturekey-us6"))}
                ),
                http,
            )
            attempt = await client.upsert_member().invoke(
                sdk.Call(CALL_ID, CONNECTION, sdk.OperationRef("mailchimp", "upsertMember")),
                mailchimp.UpsertMemberInput(LIST_ID, "ben@example.com", status_if_new="subscribed"),
            )
        assert attempt.kind == "retry", reply.status_code


class _HangingStream(httpx.AsyncByteStream):
    async def __aiter__(self):  # type: ignore[override]
        yield b"{"
        await asyncio.sleep(30)


@pytest.mark.asyncio
async def test_write_window_cutting_a_2xx_body_is_sent_and_never_resent() -> None:
    """2xx headers arrived before the write deadline cut the body, so Mailchimp accepted the send."""
    script = CampaignScript("save", lambda: httpx.Response(200, stream=_HangingStream()))
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    assert branch(await send(script, context)) == "sent"
    assert script.sends == 1


@pytest.mark.asyncio
async def test_transient_credential_failure_before_any_send_records_not_sent() -> None:
    """A transient lookup before the first send is never a defect; it records notSent and retries."""
    script = CampaignScript("save")
    context = AttemptContext()
    result = await send(script, context, credentials=FailingCredentials(RuntimeError("store unavailable")))
    assert isinstance(result, sdk.RetryError)
    assert context.marker.state == "notSent" and context.marker.send_count == 0
    assert script.requests == []


@pytest.mark.asyncio
async def test_refusal_with_an_oversized_body_is_an_unknown_outcome() -> None:
    """An unread refusal body proves nothing, so the send counts and the next attempt reads first."""
    oversized = b'{"title":"Bad Request","padding":"' + b"x" * 4_194_304 + b'"}'
    script = CampaignScript("save", lambda: httpx.Response(400, content=oversized))
    context = AttemptContext()
    assert isinstance(await send(script, context), sdk.RetryError)
    assert context.marker.state == "claimed" and context.marker.send_count == 1
    context.attempt = 2
    assert branch(await send(script, context)) == "sent"
    assert script.sends == 2


@pytest.mark.asyncio
async def test_safe_writes_retry_a_transient_credential_failure() -> None:
    """upsertMember and updateMemberTags retry a transient lookup instead of selecting defect."""
    client = mailchimp.Client(mailchimp.Config(), FailingCredentials(RuntimeError("store unavailable")))  # type: ignore[arg-type]
    call = sdk.Call(CALL_ID, CONNECTION, sdk.OperationRef("mailchimp", "upsertMember"))
    upserted = await client.upsert_member().invoke(
        call, mailchimp.UpsertMemberInput(LIST_ID, "ben@example.com", status_if_new="subscribed")
    )
    tagged = await client.update_member_tags().invoke(
        call, mailchimp.UpdateMemberTagsInput(LIST_ID, "ben@example.com", add_tags=("vip",))
    )
    assert (upserted.kind, tagged.kind) == ("retry", "retry")
    await client.aclose()
