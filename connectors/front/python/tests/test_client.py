# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused checks for hand-written Front conversion rules."""

from datetime import UTC, datetime

import pytest

from dex_connectors.front import build_reply_html
from dex_connectors.front.client import decode_conversation, timestamp
from dex_connectors.front.operations import _bare_email


def test_reply_html_uses_go_escape_entities_and_paragraphs() -> None:
    """Keep accepted reply bodies byte-compatible with the Go connector."""
    assert build_reply_html('Hi "Jane" & Tom\nLine 2\n\nIt\'s <done>') == (
        "<p>Hi &#34;Jane&#34; &amp; Tom<br>Line 2</p><p>It&#39;s &lt;done&gt;</p>"
    )


def test_front_timestamp_rounds_milliseconds_and_rejects_invalid_values() -> None:
    """Front Unix seconds retain millisecond precision."""
    assert timestamp(1767323045.1235) == datetime(2026, 1, 2, 3, 4, 5, 124000, UTC)
    with pytest.raises(ValueError):
        timestamp(-1)


def test_conversation_rejects_invalid_tag_id() -> None:
    """A malformed tag ID cannot enter a Flow result."""
    with pytest.raises(ValueError):
        decode_conversation({"id": "cnv_100", "status": "assigned", "tags": [{"id": "bad"}]})


@pytest.mark.parametrize("email", ["a..b@example.com", ".a@example.com", "a.@example.com", "a@example..com"])
def test_bare_email_matches_go_mail_parser(email: str) -> None:
    """Reject addresses Python's parser accepts but Go's parser rejects."""
    with pytest.raises(ValueError):
        _bare_email(email, "email")


@pytest.mark.asyncio
async def test_rejected_reply_token_refreshes_before_safe_resend() -> None:
    """A changed token permits a new fenced attempt after Front rejects the old one."""
    from datetime import timedelta

    import httpx
    from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef, SecretString

    from dex_connectors.front import Client, Config, Credentials, ReplyToConversationInput

    class CredentialsAfterRejection:
        def __init__(self) -> None:
            self.current = Credentials(SecretString("old-inert-token"))

        async def resolve(self, call: Call) -> Credentials:
            return self.current

        async def resolve_after_rejection(self, call: Call, driver: object) -> Credentials:
            self.current = Credentials(SecretString("new-inert-token"))
            return self.current

    class HeartbeatContext:
        attempt = 1
        deadline = datetime.now(UTC) + timedelta(seconds=30)
        marker: object | None = None

        def has_last_heartbeat_value(self) -> bool:
            return self.marker is not None

        def get_last_heartbeat_value(self, value_type: type[object]) -> object:
            return self.marker

        async def heartbeat(self, value: object) -> None:
            self.marker = value

    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(401, json={})
        return httpx.Response(201, json={"id": "com_100"})

    credential_source = CredentialsAfterRejection()
    context = HeartbeatContext()
    call_id = CallID("76e76ff6-9e73-5db4-bb93-69b6a679fae5")
    call = Call(
        call_id,
        ConnectionRef("front", "front-test"),
        OperationRef("front", "replyToConversation"),
        IdempotencyKey(call_id),
        context=context,
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider:
        operation = Client(
            Config(), credential_source, provider, api_base_url="http://127.0.0.1:22130"
        ).reply_to_conversation()
        input = ReplyToConversationInput("cnv_100", "Internal note", is_internal_note=True)
        first = await operation.invoke(call, input)
        assert first.kind == "retry"
        context.attempt = 2
        second = await operation.invoke(call, input)
        assert second.kind == "branch" and second.branch == "replied"
        assert len(requests) == 2
        assert requests[0].headers["Authorization"] == "Bearer old-inert-token"
        assert requests[1].headers["Authorization"] == "Bearer new-inert-token"
