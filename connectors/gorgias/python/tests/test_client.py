# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gorgias client validation and decoding edge cases."""

from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.gorgias import Client, Config, Credentials
from dex_connectors.gorgias.client import bare_email, decode_ticket, describe_error, parse_time
from dex_connectors.gorgias.operations import _reply_route


@pytest.mark.parametrize("value", ["ada@example.com", "agent+triage@example.com", "a@localhost"])
def test_bare_email_accepts_one_mailbox(value: str) -> None:
    assert bare_email(value)


@pytest.mark.parametrize(
    "value",
    [
        "a..b@example.com",
        "a@example..com",
        "Ada <ada@example.com>",
        "ada@example.com (old)",
        "ada@@example.com",
    ],
)
def test_bare_email_rejects_invalid_or_decorated_mailbox(value: str) -> None:
    assert not bare_email(value)


def test_provider_error_does_not_emit_message_text_or_secret() -> None:
    summary = describe_error(
        b'{"error":{"msg":"permission_denied","message":"secret-value","data":{"subject":"secret-value"}}}',
        "secret-value",
    )
    assert summary == "permission_denied; fields: subject"


def test_offsetless_timestamp_and_ticket_default_updated_at() -> None:
    created = datetime(2026, 1, 20, 9, tzinfo=UTC)
    assert parse_time("2026-01-20T09:00:00") == created
    ticket = decode_ticket({"id": 7001, "status": "open", "created_datetime": "2026-01-20T09:00:00"})
    assert ticket.created_at == ticket.updated_at == created


def test_reply_route_rejects_ambiguous_support_address() -> None:
    message = {
        "id": 1,
        "channel": "email",
        "from_agent": False,
        "source": {
            "from": {"address": "ada@example.com"},
            "to": [{"address": "help@example.com"}, {"address": "team@example.com"}],
        },
        "created_datetime": "2026-01-20T09:00:00",
    }
    with pytest.raises(ValueError, match="several addresses"):
        _reply_route([message], "Question")


def test_client_rejects_non_loopback_http_endpoint() -> None:
    reference = ConnectionRef("gorgias", "gorgias")
    credentials = StaticCredentialProvider(
        {reference: Credentials("agent@example.com", SecretString("inert-key"))}
    )
    with pytest.raises(ValueError, match="HTTPS"):
        Client(Config("acme"), credentials, api_base_url="http://example.com/api")
