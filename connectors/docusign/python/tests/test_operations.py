# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check local DocuSign validation and Go-compatible encoding boundaries."""

from datetime import UTC, datetime

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
from dex_connectors.docusign.client import path_escape, query_escape
from dex_connectors.docusign.operations import _create_body, _time


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


def test_go_path_and_query_escaping() -> None:
    """Keep Go's path-safe punctuation and escaped query tilde."""
    assert path_escape("a/b!?*:$&+=@") == "a%2Fb%21%3F%2A:$&+=@"
    assert query_escape("a b~*") == "a+b%7E%2A"


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
