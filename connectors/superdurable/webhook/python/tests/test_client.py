# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check delivery URL and stable provider id validation."""

import pytest
from dex_connectors.sdk import CallID, ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.superdurable_webhook import Client, Config, Credentials, SendEventInput


def _config(delivery_url: str) -> Config:
    return Config(
        signature_prefix="", event_id_header="", event_id_pointer="", forwarded_headers=(),
        delivery_url=delivery_url,
    )


def _credentials() -> StaticCredentialProvider[Credentials]:
    return StaticCredentialProvider({
        ConnectionRef("webhook", "forwarding"): Credentials(SecretString("inert-key")),
    })


@pytest.mark.parametrize("url", [
    "http://receiver.example.test/hook", "https://user:secret@receiver.example.test/hook",
    "https://receiver.example.test/hook#secret", "https://receiver.example.test/with space",
])
def test_rejects_unsafe_delivery_url(url: str) -> None:
    """Prevent a signed request from targeting plaintext or credential-bearing URLs."""
    with pytest.raises(ValueError):
        Client(_config(url), _credentials())


@pytest.mark.asyncio
async def test_provider_id_comes_from_stable_call_id() -> None:
    """A repeated logical send retains its receiver deduplication key."""
    client = Client(_config("https://receiver.example.test/hook"), _credentials())
    call_id = CallID("1df9f2d3-800f-54dc-898c-77640b04c7c3")
    operation = client.send_event()
    input_value = SendEventInput({"type": "form.submitted"})
    assert operation.idempotency_key(call_id, input_value) == "msg_1df9f2d3800f54dc898c77640b04c7c3"
    assert operation.idempotency_key(call_id, input_value) == operation.idempotency_key(call_id, input_value)
    await client.aclose()
