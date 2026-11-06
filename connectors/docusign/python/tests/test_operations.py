# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check local DocuSign validation and Go-compatible encoding boundaries."""

import json
from datetime import UTC, datetime
from types import SimpleNamespace

import pytest

from dex_connectors import sdk
from dex_connectors.docusign import (
    Client,
    Config,
    CreateEnvelopeFromTemplateInput,
    Credentials,
    EnvelopeCustomField,
    TemplateRole,
)
from dex_connectors.docusign.client import ProviderResponse, failure, path_escape, query_escape
from dex_connectors.docusign.operations import (
    CreateEnvelopeFromTemplateOperation,
    _create_body,
    _CreateDispatchCheckpoint,
    _go_timestamp,
    _time,
)


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
@pytest.mark.parametrize(
    "stored",
    [
        None,
        _CreateDispatchCheckpoint(False, "2026-10-04T10:00:00Z"),
        _CreateDispatchCheckpoint(True, "2026-10-04T11:00:00Z"),
    ],
)
async def test_checkpoint_controls_retry_send(
    stored: _CreateDispatchCheckpoint | None,
) -> None:
    """Cleared and false heartbeats permit resends; a dispatched one only reads back."""

    class Context:
        attempt = 2
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
    await context.heartbeat()
    if stored is not None:
        await context.heartbeat(stored)
    client = ClientStub()
    call = SimpleNamespace(id="call-1", idempotency_key="call-1", context=context)
    input = CreateEnvelopeFromTemplateInput(
        template_id="8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a",
        template_roles=(TemplateRole("Customer", "Priya Raman", "priya@example.com"),),
    )
    result = await CreateEnvelopeFromTemplateOperation(client).invoke(call, input)
    if stored is not None and stored.is_dispatched:
        assert result.kind == "uncertain"
        assert client.methods == ["GET"]
        assert client.queries[0]["from_date"] == "2026-10-04T10:00:00Z"
    else:
        assert result.branch == "created"
        assert client.methods == ["GET", "POST"]
        assert client.queries[0]["from_date"] == "2026-10-04T11:00:00Z"


@pytest.mark.asyncio
async def test_after_send_multiple_matches_preserves_original_failure() -> None:
    """Ambiguous read-back keeps the failure that triggered recovery."""

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
        call, object(), "call-1", unknown
    )
    assert result.kind == "uncertain"
    assert result.failure == unknown
