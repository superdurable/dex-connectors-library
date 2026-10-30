# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Gmail operation set bound to one logical Dex connection."""

from dataclasses import dataclass

from dex_connectors.sdk import ConnectionRef, Mutation, Query

from .models import GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput
from .operations import GetMessageOperation, ReplyToMessageOperation, SendMessageOperation
from .provider import GmailProvider


@dataclass(frozen=True, slots=True)
class Connection:
    """Logical connection, provider client, and three operations for Step factories."""

    # Stable provider and name used by Dex Web Connections.
    reference: ConnectionRef
    # Full-message read operation.
    get_message: Query[GetMessageInput, Message]
    # New-message send operation.
    send_message: Mutation[SendMessageInput, SendMessageOutput]
    # Source-thread reply operation.
    reply_to_message: Mutation[ReplyToMessageInput, SendMessageOutput]
    # Provider client retained for closing project-owned HTTP resources; mocks omit it.
    client: GmailProvider | None = None

    def __post_init__(self) -> None:
        """Require the Gmail provider and a static nonempty connection name."""
        self.reference.validate()
        if self.reference.provider != "google":
            raise ValueError("Gmail connection provider must be google")
        if self.get_message is None or self.send_message is None or self.reply_to_message is None:
            raise ValueError("all Gmail operations are required")

    async def close(self) -> None:
        """Close the provider's owned HTTP client, if this connection has one."""
        if self.client is not None:
            await self.client.close()


def new_connection(client: GmailProvider, reference: ConnectionRef) -> Connection:
    """Bind one provider client and explicit Dex ConnectionRef to all operations."""
    if client is None or reference is None:
        raise ValueError("Gmail client and connection reference are required")
    return Connection(
        reference,
        GetMessageOperation(client),
        SendMessageOperation(client),
        ReplyToMessageOperation(client),
        client,
    )


__all__ = ["Connection", "new_connection"]
