# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate Twilio configuration and credential classification boundaries."""

from dataclasses import replace

import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.twilio_messaging import Client, Config, Credentials
from dex_connectors.twilio_messaging.models import SendMessageInput
from dex_connectors.twilio_messaging.operations import _send_form

ACCOUNT = "AC" + "0123456789abcdef" * 2
CONNECTION = ConnectionRef("twilio-messaging", "twilio-messaging")


def test_client_rejects_bad_configuration() -> None:
    """Fail before provider requests for invalid identity, sender, or body limit."""
    provider = StaticCredentialProvider({CONNECTION: Credentials(auth_token=SecretString("inert"))})
    with pytest.raises(ValueError, match="accountSid"):
        Client(Config(account_sid="ACinvalid"), provider)
    with pytest.raises(ValueError, match="sender"):
        Client(Config(account_sid=ACCOUNT, default_sender="invalid"), provider)
    with pytest.raises(ValueError, match="maxResponseBytes"):
        Client(Config(account_sid=ACCOUNT, max_response_bytes=-1), provider)


def test_send_form_validates_whatsapp_and_media() -> None:
    """Preserve Go's sender pairing and public HTTPS media requirements."""
    provider = StaticCredentialProvider({CONNECTION: Credentials(auth_token=SecretString("inert"))})
    client = Client(Config(account_sid=ACCOUNT, default_sender="+14155550100"), provider)
    with pytest.raises(ValueError, match="both be whatsapp"):
        _send_form(client, SendMessageInput(to="whatsapp:+14155550123", body="hello"))
    with pytest.raises(ValueError, match="HTTPS"):
        _send_form(client, SendMessageInput(to="+14155550123", media_urls=("http://example.com/image.png",)))
    with pytest.raises(ValueError, match="1600"):
        _send_form(client, SendMessageInput(to="+14155550123", body="a" * 1601))


@pytest.mark.asyncio
async def test_invalid_credentials_are_terminal_and_lookup_failure_retries() -> None:
    """Separate bad credential material from a temporarily unavailable source."""
    call = Call(
        CallID("call-1"), CONNECTION, OperationRef("twilio-messaging", "getMessage"), IdempotencyKey("")
    )
    valid = Credentials(auth_token=SecretString("inert"))
    client = Client(
        Config(account_sid=ACCOUNT), StaticCredentialProvider({CONNECTION: replace(valid, auth_token=None)})
    )
    authorization, problem, is_transient = await client.authorization(call, "getMessage")
    assert authorization is None and problem and problem.kind == FailureKind.AUTHENTICATION
    assert not is_transient

    class UnavailableCredentials:
        async def resolve(self, call: Call) -> Credentials:
            raise ConnectionError("credential store unavailable")

    client = Client(Config(account_sid=ACCOUNT), UnavailableCredentials())
    authorization, problem, is_transient = await client.authorization(call, "getMessage")
    assert authorization is None and problem and problem.kind == FailureKind.AVAILABILITY
    assert is_transient
