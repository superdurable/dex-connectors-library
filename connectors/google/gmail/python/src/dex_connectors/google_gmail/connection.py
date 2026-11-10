# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# connectorctl: handwritten facade
"""Typed Gmail operation set bound to one logical Dex connection."""

from dex_connectors.sdk import ConnectionRef, Mutation, Query

from .definitions import (
    GET_MESSAGE_DEFINITION,
    PROVIDER,
    REPLY_TO_MESSAGE_DEFINITION,
    SEND_MESSAGE_DEFINITION,
)
from .models import GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput
from .operations import GetMessageOperation, ReplyToMessageOperation, SendMessageOperation
from .provider import GmailProvider


class Connection:
    """Nonserializable binding of the three Gmail operations to one static connection.

    Step factories and agent tools read each operation through its method, such
    as ``connection.send_message()``. The caller closes a provider-backed
    connection with ``aclose`` after its Steps stop; a mock connection owns no
    transport, so closing it has no effect.
    """

    __slots__ = ("_get_message", "_send_message", "_reply_to_message", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        get_message: Query[GetMessageInput, Message],
        send_message: Mutation[SendMessageInput, SendMessageOutput],
        reply_to_message: Mutation[ReplyToMessageInput, SendMessageOutput],
        client: GmailProvider | None = None,
    ) -> None:
        """Require the shared Google provider, a static name, and Gmail operation identities.

        Raises:
            ValueError: If the reference is invalid or not a ``google`` provider
                reference, an operation is missing, or an operation belongs to
                another Connector operation.
        """
        if reference is None:
            raise ValueError("Gmail connection reference is required")
        reference.validate()
        if reference.provider != PROVIDER:
            raise ValueError("Gmail connection provider must be google")
        if get_message is None or send_message is None or reply_to_message is None:
            raise ValueError("all Gmail operations are required")
        for operation, definition in (
            (get_message, GET_MESSAGE_DEFINITION),
            (send_message, SEND_MESSAGE_DEFINITION),
            (reply_to_message, REPLY_TO_MESSAGE_DEFINITION),
        ):
            if operation.definition().operation != definition.operation:
                raise ValueError("Gmail connection operation identity differs")
        # Stable provider and name used by Dex Web Connections.
        self.reference = reference
        self._get_message = get_message
        self._send_message = send_message
        self._reply_to_message = reply_to_message
        self._client = client

    def get_message(self) -> Query[GetMessageInput, Message]:
        """Return the full-message read Query used by an application Step or agent tool."""
        return self._get_message

    def send_message(self) -> Mutation[SendMessageInput, SendMessageOutput]:
        """Return the new-message send Mutation used by an application Step or agent tool."""
        return self._send_message

    def reply_to_message(self) -> Mutation[ReplyToMessageInput, SendMessageOutput]:
        """Return the source-thread reply Mutation used by an application Step or agent tool."""
        return self._reply_to_message

    async def aclose(self) -> None:
        """Close the provider's owned HTTP client, if this connection has one."""
        if self._client is not None:
            await self._client.close()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "gmail.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Gmail connections cannot be serialized")


def new_connection(client: GmailProvider, reference: ConnectionRef) -> Connection:
    """Bind one provider client and explicit Dex ConnectionRef to all operations.

    Raises:
        ValueError: If the client or reference is missing or invalid.
    """
    if client is None or reference is None:
        raise ValueError("Gmail client and connection reference are required")
    return Connection(
        reference,
        GetMessageOperation(client),
        SendMessageOperation(client),
        ReplyToMessageOperation(client),
        client,
    )


def new_connection_with_operations(
    get_message: Query[GetMessageInput, Message],
    send_message: Mutation[SendMessageInput, SendMessageOutput],
    reply_to_message: Mutation[ReplyToMessageInput, SendMessageOutput],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock or custom operations to the same connection identity as the real provider.

    Raises:
        ValueError: If an operation is missing or belongs to another operation,
            or the reference is not a valid ``google`` provider reference.
    """
    return Connection(reference, get_message, send_message, reply_to_message)


__all__ = ["Connection", "new_connection", "new_connection_with_operations"]
