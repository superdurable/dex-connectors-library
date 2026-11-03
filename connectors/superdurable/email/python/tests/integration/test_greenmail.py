# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise real verified SMTPS and IMAPS through CI's GreenMail service."""

import asyncio
import os
import ssl
from typing import cast
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef, SecretString

from dex_connectors.superdurable_email import (
    Client,
    Config,
    Credentials,
    GetMessageInput,
    MoveMessageInput,
    ReplyToMessageInput,
    SearchMessagesInput,
    SendMessageInput,
    SetFlagsInput,
)

from ..test_send_message import Context


class GreenMailCredentials:
    """Resolve the CI mailbox without storing credentials in a Flow value."""

    async def resolve(self, call: Call) -> Credentials:
        """Read the CI credential at the provider boundary."""
        return Credentials(
            os.environ["EMAIL_CONNECTOR_TEST_USERNAME"],
            SecretString(os.environ["EMAIL_CONNECTOR_TEST_PASSWORD"]),
            "",
            SecretString(""),
        )


def make_call(operation: str) -> Call:
    """Return a unique logical operation Call."""
    identifier = CallID(str(uuid4()))
    return Call(
        identifier,
        ConnectionRef("email", "greenmail"),
        OperationRef("email", operation),
        IdempotencyKey(identifier),
        context=cast(dex.AsyncContext, Context()),
    )


@pytest.mark.asyncio
async def test_greenmail_verified_tls_send_search_read_reply_move_and_flags() -> None:
    """Exercise all six operations through real SMTPS and IMAPS."""
    required = (
        "EMAIL_CONNECTOR_TEST_CA_CERT",
        "EMAIL_CONNECTOR_TEST_SMTPS_PORT",
        "EMAIL_CONNECTOR_TEST_IMAPS_PORT",
        "EMAIL_CONNECTOR_TEST_USERNAME",
        "EMAIL_CONNECTOR_TEST_PASSWORD",
        "EMAIL_CONNECTOR_TEST_ADDRESS",
    )
    if any(name not in os.environ for name in required):
        pytest.skip("GreenMail service variables are supplied by CI")
    context = ssl.create_default_context(cafile=os.environ["EMAIL_CONNECTOR_TEST_CA_CERT"])
    host = os.environ.get("EMAIL_CONNECTOR_TEST_IMAP_HOST", "127.0.0.1")
    smtp_host = os.environ.get("EMAIL_CONNECTOR_TEST_SMTP_HOST", "127.0.0.1")
    config = Config(
        host,
        smtp_host,
        os.environ["EMAIL_CONNECTOR_TEST_ADDRESS"],
        "",
        int(os.environ["EMAIL_CONNECTOR_TEST_IMAPS_PORT"]),
        "implicitTLS",
        int(os.environ["EMAIL_CONNECTOR_TEST_SMTPS_PORT"]),
        "implicitTLS",
    )
    client = Client(config, GreenMailCredentials(), tls_context=context)
    subject = "Dex Python email integration " + uuid4().hex
    send = await client.send_message().invoke(
        make_call("sendMessage"),
        SendMessageInput(
            to=(os.environ["EMAIL_CONNECTOR_TEST_ADDRESS"],),
            reply_to="connector+reply@example.com",
            subject=subject,
            text="Integration body",
        ),
    )
    assert send.branch == "sent", send.failure
    assert send.value is not None
    search = None
    for _ in range(40):
        search = await client.search_messages().invoke(
            make_call("searchMessages"), SearchMessagesInput(subject_contains=subject)
        )
        if search.branch == "searched" and search.value is not None and search.value.messages:
            break
        await asyncio.sleep(0.25)
    assert search is not None and search.branch == "searched", search.failure if search else None
    assert search.value is not None and search.value.messages
    reference = search.value.messages[0].reference
    found = await client.get_message().invoke(make_call("getMessage"), GetMessageInput(reference))
    assert found.branch == "found", found.failure
    assert found.value is not None and "Integration body" in found.value.text
    updated = await client.set_flags().invoke(make_call("setFlags"), SetFlagsInput(reference, is_seen=True))
    assert updated.branch == "updated", updated.failure
    assert updated.value is not None and updated.value.is_seen
    reply = await client.reply_to_message().invoke(
        make_call("replyToMessage"), ReplyToMessageInput(reference, "Integration reply")
    )
    assert reply.branch == "sent", reply.failure
    assert reply.value is not None and reply.value.in_reply_to == send.value.message_id

    archive = "Archive_" + uuid4().hex
    credentials = await client.resolve_credentials(make_call("moveMessage"), "moveMessage")
    session = client.open_imap(credentials, "moveMessage")
    try:
        status, _ = session.create(archive)
        assert status == "OK"
    finally:
        session.logout()
    moved = await client.move_message().invoke(
        make_call("moveMessage"), MoveMessageInput(reference, archive, send.value.message_id)
    )
    assert moved.branch == "moved", moved.failure
    assert moved.value is not None and moved.value.destination.uid_validity > 0
    assert moved.value.destination.uid > 0
    destination = await client.get_message().invoke(
        make_call("getMessage"), GetMessageInput(moved.value.destination)
    )
    assert destination.branch == "found", destination.failure
    assert destination.value is not None and destination.value.message_id == send.value.message_id
