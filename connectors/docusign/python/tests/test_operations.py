# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check local DocuSign validation and Go-compatible encoding boundaries."""

import json
from datetime import UTC, datetime
from types import SimpleNamespace
from typing import Any

import httpx
import pytest
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.providerhttp import MAX_ERROR_BODY_BYTES, ProviderTransportError

from dex_connectors import sdk
from dex_connectors.docusign import (
    Client,
    Config,
    CreateEnvelopeFromTemplateInput,
    Credentials,
    EnvelopeCustomField,
    TemplateRole,
    VoidEnvelopeInput,
)
from dex_connectors.docusign._generated.connection import (
    CredentialMaterialError,
    decode_credentials,
)
from dex_connectors.docusign.client import (
    ProviderResponse,
    Session,
    SessionFailure,
    failure,
    path_escape,
    query_escape,
)
from dex_connectors.docusign.operations import (
    CreateEnvelopeFromTemplateOperation,
    VoidEnvelopeOperation,
    _create_body,
    _CreateDispatchCheckpoint,
    _CreateSendBudget,
    _go_timestamp,
    _time,
)


def sdk_decoding_failure() -> ValueError:
    """Return the SDK project provider's error after the generated decoder rejects malformed material."""
    try:
        try:
            decode_credentials({"unknown_field": "value"})
        except ValueError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("malformed material decoded")


@pytest.mark.parametrize(
    "value",
    [
        "2026-02-30T12:00:00Z",
        "2025-02-29T12:00:00Z",
        "2026-01-01T24:00:00Z",
        "2026-01-01T12:00:00+01:60",
        "2026-01-01 12:00:00Z",
    ],
)
def test_invalid_provider_timestamp(value: str) -> None:
    """Reject calendar and clock values that Go time.Parse rejects."""
    with pytest.raises(ValueError):
        _time(value)


def test_extended_offset_and_comma_fraction() -> None:
    """Match Go's wide RFC 3339 offset and comma fraction acceptance."""
    assert _time("2026-01-05T12:00:00,25+99:59") == datetime(2026, 1, 1, 8, 1, 0, 250000, UTC)


def test_go_path_escaping() -> None:
    """Keep Go's path-safe punctuation."""
    assert path_escape("a/b!?*:$&+=@") == "a%2Fb%21%3F%2A:$&+=@"


@pytest.mark.parametrize(
    ("value", "escaped"),
    [
        ("a b~*+", "a+b~%2A%2B"),
        ("AZaz09-._~", "AZaz09-._~"),
        ("! '()*", "%21+%27%28%29%2A"),
        ("a&b=c/d?e", "a%26b%3Dc%2Fd%3Fe"),
        ("é 雪", "%C3%A9+%E9%9B%AA"),
    ],
)
def test_go_query_escaping(value: str, escaped: str) -> None:
    """Match values recorded from Go url.QueryEscape."""
    assert query_escape(value) == escaped


def test_create_request_contains_only_provider_fields() -> None:
    """Include the hidden marker and omit optional blank fields."""
    input = CreateEnvelopeFromTemplateInput(
        template_id="8C9F5A8B-1111-2222-3333-4F5E6D7C8B9A",
        template_roles=(TemplateRole("Customer", "Priya Raman", "priya@example.com", 2),),
        custom_fields=(EnvelopeCustomField("correlation", "opp-123"),),
    )
    body = _create_body(input, "call-id")
    assert set(body) == {"templateId", "templateRoles", "status", "customFields"}
    assert body["templateId"] == input.template_id.lower()
    assert body["templateRoles"][0]["routingOrder"] == "2"
    assert body["customFields"]["textCustomFields"][0]["value"] == "call-id"


@pytest.mark.parametrize("email", ["bad", "a@b", "a b@example.com", "a@example.com, b@example.com"])
def test_create_rejects_bad_email(email: str) -> None:
    """Reject inputs DocuSign could not treat as one plain address."""
    input = CreateEnvelopeFromTemplateInput(
        template_id="8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a",
        template_roles=(TemplateRole("Customer", "Priya Raman", email),),
    )
    with pytest.raises(ValueError):
        _create_body(input, "call-id")


@pytest.mark.asyncio
async def test_zero_limits_use_manifest_defaults() -> None:
    """Match Go's zero-value Config defaults for all three limits."""
    client = Client(
        Config(max_response_bytes=0, max_document_bytes=0, connect_max_body_bytes=0),
        sdk.StaticCredentialProvider[Credentials]({}),
    )
    try:
        assert client.config == Config()
    finally:
        await client.aclose()


def test_checkpoint_timestamp_trims_fractional_zeros() -> None:
    assert _go_timestamp(datetime(2026, 10, 4, 12, tzinfo=UTC)) == "2026-10-04T12:00:00Z"
    assert _go_timestamp(datetime(2026, 10, 4, 12, 0, 0, 120000, UTC)) == "2026-10-04T12:00:00.12Z"


@pytest.mark.asyncio
@pytest.mark.parametrize("attempt", [2, 4])
@pytest.mark.parametrize(
    "stored",
    [
        None,
        _CreateDispatchCheckpoint(False, "2026-10-04T10:00:00Z"),
        _CreateDispatchCheckpoint(True, "2026-10-04T11:00:00Z"),
    ],
)
async def test_checkpoint_controls_retry_send(stored: _CreateDispatchCheckpoint | None, attempt: int) -> None:
    """A later attempt reads back, resends below the cap of 3, and selects uncertain at the cap."""

    class Context:
        first_attempt_at = datetime(2026, 10, 4, 12, tzinfo=UTC)
        deadline = None

        def __init__(self) -> None:
            self.value = _CreateDispatchCheckpoint(True, "2026-10-04T11:00:00Z")

        def has_last_heartbeat_value(self) -> bool:
            return True

        def get_last_heartbeat_value(self, _type: object) -> _CreateDispatchCheckpoint | None:
            return self.value

        async def heartbeat(self, value: _CreateDispatchCheckpoint | None = None) -> None:
            self.value = value

    class ClientStub:
        def __init__(self) -> None:
            self.methods: list[str] = []
            self.queries: list[dict[str, str] | None] = []

        async def open_session(self, _call: object, _operation: str) -> object:
            return object()

        async def request(
            self,
            _call: object,
            _session: object,
            method: str,
            _path: str,
            query: dict[str, str] | None = None,
            **_kwargs: object,
        ) -> ProviderResponse:
            self.methods.append(method)
            self.queries.append(query)
            body = (
                b'{"envelopes":[]}'
                if method == "GET"
                else json.dumps(
                    {"envelopeId": "8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a", "status": "sent"}
                ).encode()
            )
            return ProviderResponse(200 if method == "GET" else 201, {}, body)

        def now(self) -> datetime:
            return datetime(2026, 10, 4, 12, tzinfo=UTC)

        def receipt(self, _envelope_id: str = "") -> sdk.Receipt:
            return sdk.Receipt()

    context = Context()
    context.attempt = attempt
    await context.heartbeat()
    if stored is not None:
        await context.heartbeat(stored)
    client = ClientStub()
    call = SimpleNamespace(id="call-1", idempotency_key="call-1", context=context, max_sends=3)
    input = CreateEnvelopeFromTemplateInput(
        template_id="8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a",
        template_roles=(TemplateRole("Customer", "Priya Raman", "priya@example.com"),),
    )
    result = await CreateEnvelopeFromTemplateOperation(client).invoke(call, input)
    is_dispatched = stored is not None and stored.is_dispatched
    expected_from_date = "2026-10-04T10:00:00Z" if is_dispatched else "2026-10-04T11:00:00Z"
    assert client.queries[0]["from_date"] == expected_from_date
    if attempt > 3:
        assert result.kind == "uncertain"
        assert client.methods == ["GET"]
    else:
        assert result.branch == "created"
        assert client.methods == ["GET", "POST"]
        if is_dispatched:
            assert stored is not None and context.value is not None
            # The resend keeps the earliest dispatch time.
            assert context.value.dispatched_at == stored.dispatched_at
            assert (context.value.attempt, context.value.send_count) == (attempt, attempt)


@pytest.mark.asyncio
async def test_after_send_multiple_matches_selects_uncertain_without_resending() -> None:
    """Several tagged envelopes are duplicates already present, so no resend is allowed."""

    class ClientStub:
        def now(self) -> datetime:
            return datetime(2026, 10, 4, 12, tzinfo=UTC)

        def receipt(self, _envelope_id: str = "") -> sdk.Receipt:
            return sdk.Receipt()

        async def request(
            self, _call: object, _session: object, _method: str, _path: str, _query: object, **_kwargs: object
        ) -> ProviderResponse:
            envelope = {
                "envelopeId": "8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a",
                "status": "sent",
                "customFields": {"textCustomFields": [{"name": "dexIdempotencyKey", "value": "call-1"}]},
            }
            return ProviderResponse(200, {}, json.dumps({"envelopes": [envelope, envelope]}).encode())

    context = SimpleNamespace(
        deadline=None,
        first_attempt_at=datetime(2026, 10, 4, 12, tzinfo=UTC),
        has_last_heartbeat_value=lambda: False,
    )
    call = SimpleNamespace(context=context)
    unknown = failure("createEnvelopeFromTemplate", sdk.FailureKind.AVAILABILITY, "original create failure")
    result = await CreateEnvelopeFromTemplateOperation(ClientStub())._recover_or_uncertain(
        call, object(), "call-1", _DISPATCHED, _CreateSendBudget(1, 0, 3, False), unknown
    )
    assert result.kind == "uncertain"
    assert result.failure is not None and result.failure.kind == sdk.FailureKind.CONFLICT


@pytest.mark.asyncio
@pytest.mark.parametrize("can_resend_later,expected", [(True, "retry"), (False, "uncertain")])
async def test_after_send_miss_resends_below_the_cap(can_resend_later: bool, expected: str) -> None:
    """A read-back miss after an unknown send retries below the cap and is uncertain at it."""

    class ClientStub:
        def now(self) -> datetime:
            return datetime(2026, 10, 4, 12, tzinfo=UTC)

        def receipt(self, _envelope_id: str = "") -> sdk.Receipt:
            return sdk.Receipt()

        async def request(
            self, _call: object, _session: object, _method: str, _path: str, _query: object, **_kwargs: object
        ) -> ProviderResponse:
            return ProviderResponse(200, {}, b'{"envelopes":[]}')

    context = SimpleNamespace(
        deadline=None,
        first_attempt_at=datetime(2026, 10, 4, 12, tzinfo=UTC),
        has_last_heartbeat_value=lambda: False,
    )
    unknown = failure("createEnvelopeFromTemplate", sdk.FailureKind.AVAILABILITY, "original create failure")
    sends = _CreateSendBudget(1, 0 if can_resend_later else 2, 3, False)
    result = await CreateEnvelopeFromTemplateOperation(ClientStub())._recover_or_uncertain(
        SimpleNamespace(context=context), object(), "call-1", _DISPATCHED, sends, unknown
    )
    assert result.kind == expected
    assert result.failure == unknown


_DISPATCHED = _CreateDispatchCheckpoint(True, "2026-10-04T12:00:00Z", 1, 1, False)
_ENVELOPE_ID = "8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a"


class _AttemptContext:
    """One attempt of a Step execution, starting from the heartbeat its previous attempt left."""

    first_attempt_at = datetime(2026, 10, 4, 12, tzinfo=UTC)
    deadline = None

    def __init__(self, attempt: int, value: _CreateDispatchCheckpoint | None) -> None:
        self.attempt = attempt
        self.value = value

    def has_last_heartbeat_value(self) -> bool:
        return self.value is not None

    def get_last_heartbeat_value(self, _type: object) -> _CreateDispatchCheckpoint | None:
        return self.value

    async def heartbeat(self, value: _CreateDispatchCheckpoint | None = None) -> None:
        self.value = value


class _ScriptedDocuSign:
    """Answer each POST from a script and every listing with no tagged envelope."""

    def __init__(self, posts: list[object], envelopes: dict[str, bytes] | None = None) -> None:
        self.posts = posts
        self.envelopes = envelopes or {}
        self.methods: list[str] = []
        self.paths: list[str] = []

    async def open_session(self, _call: object, _operation: str) -> object:
        return object()

    async def request(
        self, _call: object, _session: object, method: str, path: str, *_args: object, **_kwargs: object
    ) -> ProviderResponse:
        self.methods.append(method)
        self.paths.append(method + " " + path)
        if method == "GET" and path in self.envelopes:
            return ProviderResponse(200, {}, self.envelopes[path])
        if method == "GET" and path != "/envelopes":
            return _docusign_error(404, "ENVELOPE_DOES_NOT_EXIST")
        if method == "GET":
            return ProviderResponse(200, {}, b'{"envelopes":[]}')
        answer = self.posts.pop(0)
        if isinstance(answer, Exception):
            raise answer
        assert isinstance(answer, ProviderResponse)
        return answer

    def classify(self, operation: str, response: ProviderResponse) -> object:
        return Client.classify(self, operation, response)  # type: ignore[arg-type]

    def now(self) -> datetime:
        return datetime(2026, 10, 4, 12, tzinfo=UTC)

    def receipt(self, _envelope_id: str = "", _error_code: str = "") -> sdk.Receipt:
        return sdk.Receipt()


async def _run_create_attempt(
    provider: _ScriptedDocuSign, attempt: int, heartbeat: _CreateDispatchCheckpoint | None
) -> tuple[sdk.MutationAttempt[object], _CreateDispatchCheckpoint | None]:
    context = _AttemptContext(attempt, heartbeat)
    call = SimpleNamespace(id="call-1", idempotency_key="call-1", context=context, max_sends=3)
    input = CreateEnvelopeFromTemplateInput(
        template_id=_ENVELOPE_ID,
        template_roles=(TemplateRole("Customer", "Priya Raman", "priya@example.com"),),
    )
    result = await CreateEnvelopeFromTemplateOperation(provider).invoke(call, input)  # type: ignore[arg-type]
    return result, context.value  # type: ignore[return-value]


def _docusign_error(status: int, code: str) -> ProviderResponse:
    return ProviderResponse(status, {}, json.dumps({"errorCode": code, "message": "inert"}).encode())


@pytest.mark.asyncio
async def test_rate_limited_resends_are_not_counted_and_each_resend_reads_back_first() -> None:
    created = ProviderResponse(201, {}, json.dumps({"envelopeId": _ENVELOPE_ID, "status": "sent"}).encode())
    provider = _ScriptedDocuSign(
        [
            _docusign_error(500, "UNSPECIFIED_ERROR"),
            _docusign_error(429, "BURST_APIINVOCATION_LIMIT_EXCEEDED"),
            _docusign_error(429, "BURST_APIINVOCATION_LIMIT_EXCEEDED"),
            created,
        ]
    )
    heartbeat = None
    for attempt in (1, 2, 3):
        result, heartbeat = await _run_create_attempt(provider, attempt, heartbeat)
        assert result.kind == "retry", attempt
        # Every attempt after the unknown send reads back first.
        assert provider.methods.count("GET") == attempt
    assert heartbeat is not None and heartbeat.send_count == 1, "the rate-limited resends are not counted"
    result, _ = await _run_create_attempt(provider, 4, heartbeat)
    assert result.branch == "created", "one counted send leaves room below the cap of three"
    assert provider.methods.count("GET") == 4 and provider.methods.count("POST") == 4


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "answer",
    [
        ProviderResponse(201, {}, b'{"envelopeId":'),
        ProviderTransportError(False, 201),
    ],
    ids=["unusable summary", "body cut off after the headers"],
)
async def test_an_accepted_create_is_never_sent_again(answer: object) -> None:
    provider = _ScriptedDocuSign([answer])
    result, heartbeat = await _run_create_attempt(provider, 1, None)
    assert result.kind == "retry" and heartbeat is not None and heartbeat.is_accepted
    result, heartbeat = await _run_create_attempt(provider, 2, heartbeat)
    assert result.kind == "retry", "a later attempt only reads back"
    result, _ = await _run_create_attempt(provider, 3, heartbeat)
    assert result.kind == "uncertain", "the read-back attempts reached the cap"
    assert provider.methods.count("POST") == 1 and provider.methods.count("GET") == 3


@pytest.mark.asyncio
async def test_service_unavailable_retries_without_counting_a_send() -> None:
    provider = _ScriptedDocuSign([_docusign_error(503, "UNSPECIFIED_ERROR")])
    result, heartbeat = await _run_create_attempt(provider, 1, None)
    assert result.kind == "retry"
    assert provider.methods == ["POST"], "DocuSign answers 503 before it processes the create"
    assert heartbeat is not None and not heartbeat.is_dispatched and heartbeat.send_count == 0


@pytest.mark.asyncio
async def test_refusal_after_a_lost_checkpoint_selects_uncertain() -> None:
    provider = _ScriptedDocuSign([_docusign_error(400, "TEMPLATE_ID_INVALID")])
    result, _ = await _run_create_attempt(provider, 2, None)
    assert result.kind == "uncertain", "the first attempt may have sent before Dex stored its checkpoint"
    assert provider.methods == ["GET", "POST"]


class _AcceptedMarkerLostContext(_AttemptContext):
    """Fail the heartbeat that would record the accepted marker."""

    async def heartbeat(self, value: _CreateDispatchCheckpoint | None = None) -> None:
        if value is not None and value.is_accepted:
            raise RuntimeError("the heartbeat could not be recorded")
        self.value = value


@pytest.mark.asyncio
async def test_accepted_create_whose_marker_is_lost_selects_uncertain() -> None:
    provider = _ScriptedDocuSign([ProviderResponse(201, {}, b'{"envelopeId":')])
    call = SimpleNamespace(
        id="call-1", idempotency_key="call-1", context=_AcceptedMarkerLostContext(1, None), max_sends=3
    )
    input = CreateEnvelopeFromTemplateInput(
        template_id=_ENVELOPE_ID,
        template_roles=(TemplateRole("Customer", "Priya Raman", "priya@example.com"),),
    )
    result = await CreateEnvelopeFromTemplateOperation(provider).invoke(call, input)  # type: ignore[arg-type]
    assert result.kind == "uncertain", "a later attempt could not know the create was accepted"
    assert provider.methods == ["POST", "GET"]


@pytest.mark.asyncio
async def test_refusal_that_proves_nothing_resends_below_the_cap() -> None:
    answer = ProviderResponse(400, {}, b"", True)
    provider = _ScriptedDocuSign([answer, answer, answer])
    result, heartbeat = await _run_create_attempt(provider, 1, None)
    assert result.kind == "retry" and provider.methods == ["POST", "GET"]
    result, heartbeat = await _run_create_attempt(provider, 2, heartbeat)
    assert result.kind == "retry"
    result, _ = await _run_create_attempt(provider, 3, heartbeat)
    assert result.kind == "uncertain", "the third send reached the cap"
    assert provider.methods.count("POST") == 3


def _envelope_naming_answer(status: int) -> ProviderResponse:
    body = {"errorCode": "UNSPECIFIED_ERROR", "message": "inert", "envelopeId": _ENVELOPE_ID.upper()}
    return ProviderResponse(status, {}, json.dumps(body).encode())


def _stored_envelope(marker_value: str) -> bytes:
    return json.dumps(
        {
            "envelopeId": _ENVELOPE_ID,
            "status": "sent",
            "statusChangedDateTime": "2026-10-04T11:00:00.0000000Z",
            "customFields": {"textCustomFields": [{"name": "dexIdempotencyKey", "value": marker_value}]},
        }
    ).encode()


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [400, 422, 500])
async def test_refusal_naming_an_envelope_is_accepted_and_read_back_by_id(status: int) -> None:
    provider = _ScriptedDocuSign(
        [_envelope_naming_answer(status)], {"/envelopes/" + _ENVELOPE_ID: _stored_envelope("call-1")}
    )
    result, heartbeat = await _run_create_attempt(provider, 1, None)
    assert result.branch == "created"
    assert result.value.envelope_id == _ENVELOPE_ID and result.value.was_recovered  # type: ignore[attr-defined]
    assert provider.paths == ["POST /envelopes", "GET /envelopes/" + _ENVELOPE_ID]
    assert heartbeat is not None and heartbeat.is_accepted


@pytest.mark.asyncio
async def test_refusal_naming_an_envelope_is_never_sent_again() -> None:
    provider = _ScriptedDocuSign([_envelope_naming_answer(422)])
    result, heartbeat = await _run_create_attempt(provider, 1, None)
    assert result.kind == "retry" and heartbeat is not None and heartbeat.is_accepted
    result, heartbeat = await _run_create_attempt(provider, 2, heartbeat)
    assert result.kind == "retry", "a later attempt only reads back"
    result, _ = await _run_create_attempt(provider, 3, heartbeat)
    assert result.kind == "uncertain"
    assert provider.methods.count("POST") == 1


class _RefreshingCredentials:
    """Resolve one token and count forced refreshes after a 401."""

    def __init__(self) -> None:
        self.refreshes = 0

    async def resolve(self, _call: object) -> Credentials:
        return Credentials(access_token=sdk.SecretString("token"))

    async def resolve_after_rejection(self, _call: object) -> Credentials:
        self.refreshes += 1
        return Credentials(access_token=sdk.SecretString("new-token"))


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [json.dumps({"errorCode": "USER_AUTHENTICATION_FAILED", "envelopeId": _ENVELOPE_ID}).encode(), None],
    ids=["naming an envelope", "cut off"],
)
async def test_client_does_not_refresh_and_resend_after_an_unprovable_401(body: bytes | None) -> None:
    posts: list[httpx.Request] = []

    async def stream() -> Any:
        yield b'{"errorCode":"USER_AUTHENTICATION_FAILED"'
        raise httpx.ReadError("connection lost")

    def respond(request: httpx.Request) -> httpx.Response:
        posts.append(request)
        if body is None:
            return httpx.Response(401, content=stream())
        return httpx.Response(401, content=body)

    credentials = _RefreshingCredentials()
    client = Client(
        Config(),
        credentials,  # type: ignore[arg-type]
        http_client=httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    try:
        token = Credentials(access_token=sdk.SecretString("token"))
        session = Session(_ENVELOPE_ID, "https://na3.docusign.net", token)
        response = await client.request(
            SimpleNamespace(),  # type: ignore[arg-type]
            session,
            "POST",
            "/envelopes",
            body={},
        )
    finally:
        await client.aclose()
    assert response.status == 401
    assert len(posts) == 1 and credentials.refreshes == 0, "no second POST with a refreshed token"


class _SessionFailingDocuSign(_ScriptedDocuSign):
    """Fail every session with a terminal credential failure."""

    async def open_session(self, _call: object, operation: str) -> object:
        raise SessionFailure(
            "defect", failure(operation, sdk.FailureKind.AUTHENTICATION, "authorize the connection again")
        )


@pytest.mark.asyncio
@pytest.mark.parametrize("attempt", [2, 3])
async def test_terminal_session_failure_after_a_possible_send_selects_uncertain(attempt: int) -> None:
    provider = _SessionFailingDocuSign([])
    result, heartbeat = await _run_create_attempt(provider, attempt, _DISPATCHED)
    assert result.kind == "uncertain"
    assert heartbeat == _DISPATCHED and provider.methods == []


class _FailingCredentials:
    """Fail every resolve with error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, _call: object) -> Credentials:
        raise self.error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "expected"),
    [
        (CredentialBrokerUnavailableError("broker is unavailable"), "retry"),
        (ObjectNotFoundError("missing"), "defect"),
        (ConnectionNotSelectedError(), "defect"),
        (CredentialBrokerRejectedError(), "defect"),
        (sdk.ReauthorizationRequiredError(), "defect"),
        (CredentialMaterialError("DocuSign stored credential does not match"), "defect"),
        (sdk_decoding_failure(), "defect"),
        (ValueError("unlisted"), "retry"),
    ],
    ids=[
        "broker unavailable",
        "credentials missing",
        "connection unselected",
        "broker rejected",
        "reauth",
        "malformed",
        "sdk decoding",
        "unlisted",
    ],
)
async def test_open_session_classifies_credential_failures(error: Exception, expected: str) -> None:
    client = Client(Config(), _FailingCredentials(error))  # type: ignore[arg-type]
    try:
        with pytest.raises(SessionFailure) as raised:
            await client.open_session(SimpleNamespace(), "createEnvelopeFromTemplate")  # type: ignore[arg-type]
    finally:
        await client.aclose()
    assert raised.value.category == expected


@pytest.mark.asyncio
async def test_void_retries_a_refusal_with_an_incomplete_body() -> None:
    class _VoidDocuSign(_ScriptedDocuSign):
        async def request(self, *_args: object, **_kwargs: object) -> ProviderResponse:
            return ProviderResponse(400, {}, b"", True)

    call = SimpleNamespace(id="call-1", idempotency_key="call-1", context=_AttemptContext(1, None))
    result = await VoidEnvelopeOperation(_VoidDocuSign([])).invoke(  # type: ignore[arg-type]
        call,  # type: ignore[arg-type]
        VoidEnvelopeInput(envelope_id=_ENVELOPE_ID, voided_reason="Deal lost"),
    )
    assert result.kind == "retry"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [b'{"errorCode":"TEMPLATE_ID_INVALID"}' + b" " * MAX_ERROR_BODY_BYTES, None],
    ids=["oversized", "cut off"],
)
async def test_client_marks_an_incomplete_error_body(body: bytes | None) -> None:
    async def stream() -> Any:
        yield b'{"errorCode":"TEMPLATE_ID_INVALID"'
        raise httpx.ReadError("connection lost")

    def respond(_request: httpx.Request) -> httpx.Response:
        if body is None:
            return httpx.Response(400, content=stream())
        return httpx.Response(400, content=body)

    client = Client(
        Config(),
        sdk.StaticCredentialProvider[Credentials]({}),
        http_client=httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    try:
        credentials = Credentials(access_token=sdk.SecretString("token"))
        session = Session(_ENVELOPE_ID, "https://na3.docusign.net", credentials)
        response = await client.request(
            SimpleNamespace(),  # type: ignore[arg-type]
            session,
            "POST",
            "/envelopes",
            body={},
        )
    finally:
        await client.aclose()
    assert response.status == 400 and response.is_error_body_incomplete


@pytest.mark.asyncio
async def test_first_send_refusal_records_no_send() -> None:
    provider = _ScriptedDocuSign([_docusign_error(400, "TEMPLATE_ID_INVALID")])
    result, heartbeat = await _run_create_attempt(provider, 1, None)
    assert result.branch == "providerRejected"
    assert heartbeat is not None and not heartbeat.is_dispatched and heartbeat.send_count == 0
