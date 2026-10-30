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
    """Logical connection plus all three operation implementations for Step factories."""

    # Stable provider and name used by Dex Web Connections.
    reference: ConnectionRef
    # Full-message read operation.
    get_message: Query[GetMessageInput, Message]
    # New-message send operation.
    send_message: Mutation[SendMessageInput, SendMessageOutput]
    # Source-thread reply operation.
    reply_to_message: Mutation[ReplyToMessageInput, SendMessageOutput]

    def __post_init__(self) -> None:
        """Require the Gmail provider and a static nonempty connection name."""
        self.reference.validate()
        if self.reference.provider != "google":
            raise ValueError("Gmail connection provider must be google")
        if self.get_message is None or self.send_message is None or self.reply_to_message is None:
            raise ValueError("all Gmail operations are required")


def new_connection(provider: GmailProvider, connection_name: str) -> Connection:
    """Bind all Gmail operations to one static Dex Web connection name."""
    if provider is None or not connection_name.strip():
        raise ValueError("Gmail provider and static connection name are required")
    return Connection(
        ConnectionRef("google", connection_name),
        GetMessageOperation(provider),
        SendMessageOperation(provider),
        ReplyToMessageOperation(provider),
    )


__all__ = ["Connection", "new_connection"]
