# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check delivery URL and stable provider id validation."""

from dataclasses import replace

import httpx
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


def test_canonicalizes_empty_fragment() -> None:
    """Return the URL representation used by Go after parsing."""
    client = Client(_config("https://receiver.example.test/hook#"), _credentials())
    assert client.delivery_url == "https://receiver.example.test/hook"


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


def _call() -> Call:
    return Call(
        CallID("1df9f2d3-800f-54dc-898c-77640b04c7c3"),
        ConnectionRef("webhook", "forwarding"),
        OperationRef("webhook", "sendEvent"),
        IdempotencyKey("msg_1df9f2d3800f54dc898c77640b04c7c3"),
    )


@pytest.mark.asyncio
async def test_unexpected_credential_error_is_authentication_defect() -> None:
    """Map a credential provider failure to the same definite defect as Go."""
    class FailingCredentials:
        async def resolve(self, call: Call) -> None:
            raise RuntimeError("credential source failed")

    client = Client(_config("https://receiver.example.test/hook"), FailingCredentials())  # type: ignore[arg-type]
    attempt = await client.send_event().invoke(_call(), SendEventInput(b"null"))
    assert attempt.branch == "defect"
    assert attempt.failure is not None and attempt.failure.kind == FailureKind.AUTHENTICATION
    await client.aclose()


@pytest.mark.asyncio
async def test_request_construction_error_is_local_defect(monkeypatch: pytest.MonkeyPatch) -> None:
    """Classify a request that could not be constructed before dispatch."""
    client = Client(_config("https://receiver.example.test/hook"), _credentials())

    def fail_build_request(*args: object, **kwargs: object) -> None:
        raise ValueError("invalid request")

    monkeypatch.setattr(client.http_client, "build_request", fail_build_request)
    attempt = await client.send_event().invoke(_call(), SendEventInput(b"null"))
    assert attempt.branch == "defect"
    assert attempt.failure is not None and attempt.failure.kind == FailureKind.LOCAL_DEFECT
    await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize("payload,expected_body", [(None, b"null"), (b"  null  ", b"  null  ")])
async def test_json_null_payload_is_sent_without_reserialization(
    payload: object, expected_body: bytes,
) -> None:
    """Send the caller's exact JSON bytes, including a valid null payload."""
    received_bodies: list[bytes] = []

    def answer(request: httpx.Request) -> httpx.Response:
        received_bodies.append(request.content)
        return httpx.Response(202)

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(_config("https://receiver.example.test/hook"), _credentials(), http_client)
        attempt = await client.send_event().invoke(_call(), SendEventInput(payload))
        assert attempt.branch == "delivered"
        assert received_bodies == [expected_body]


@pytest.mark.asyncio
async def test_body_limit_uses_original_json_byte_count() -> None:
    """Apply maxBodyBytes to the bytes that would be signed and sent."""
    config = replace(_config("https://receiver.example.test/hook"), max_body_bytes=4)
    client = Client(config, _credentials())
    attempt = await client.send_event().invoke(_call(), SendEventInput(b"  null  "))
    assert attempt.branch == "defect"
    assert attempt.failure is not None and attempt.failure.kind == FailureKind.VALIDATION
    await client.aclose()


@pytest.mark.asyncio
async def test_whsec_secret_accepts_base64_line_breaks() -> None:
    """Accept CRLF in Standard Webhooks secrets as Go's StdEncoding does."""
    credentials = StaticCredentialProvider({
        ConnectionRef("webhook", "forwarding"): Credentials(
            SecretString("whsec_c3RhbmRhcmQtd2Vi\r\naG9va3MtdGVzdC1rZXk="),
        ),
    })
    transport = httpx.MockTransport(lambda request: httpx.Response(202))
    async with httpx.AsyncClient(transport=transport) as http_client:
        client = Client(_config("https://receiver.example.test/hook"), credentials, http_client)
        attempt = await client.send_event().invoke(_call(), SendEventInput(b"null"))
        assert attempt.branch == "delivered"
