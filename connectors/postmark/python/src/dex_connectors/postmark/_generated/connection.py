# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation bindings for one named Dex connection."""

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_MESSAGE_DETAILS_DEFINITION, LIST_OUTBOUND_MESSAGES_DEFINITION
from ..models import GetMessageDetailsInput, ListOutboundMessagesInput, MessageDetails, MessagePage

class Connection:
    """Nonserializable Postmark operation binding."""

    __slots__ = ('_list_outbound_messages', '_get_message_details', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_outbound_messages: Query[ListOutboundMessagesInput, MessagePage],
        get_message_details: Query[GetMessageDetailsInput, MessageDetails],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_outbound_messages is None or get_message_details is None:
            raise ValueError("Postmark connection provider and operations are required")
        if list_outbound_messages.definition().operation != LIST_OUTBOUND_MESSAGES_DEFINITION.operation:
            raise ValueError("Postmark connection operation identity differs")
        if get_message_details.definition().operation != GET_MESSAGE_DETAILS_DEFINITION.operation:
            raise ValueError("Postmark connection operation identity differs")
        self.reference = reference
        self._list_outbound_messages = list_outbound_messages
        self._get_message_details = get_message_details
        self._client = client

    def list_outbound_messages(self) -> Query[ListOutboundMessagesInput, MessagePage]:
        """Return the list_outbound_messages operation for a Dex Step."""
        return self._list_outbound_messages

    def get_message_details(self) -> Query[GetMessageDetailsInput, MessageDetails]:
        """Return the get_message_details operation for a Dex Step."""
        return self._get_message_details

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "postmark.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Postmark connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.postmark.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a provider client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Postmark client is required")
    return Connection(reference, client.list_outbound_messages(), client.get_message_details(), client)

setattr(new_connection, '__module__', 'dex_connectors.postmark.connection')


def new_connection_with_operations(
    list_outbound_messages: Query[ListOutboundMessagesInput, MessagePage],
    get_message_details: Query[GetMessageDetailsInput, MessageDetails],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same logical connection identity."""
    return Connection(reference, list_outbound_messages, get_message_details)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.postmark.connection')


class _ProjectCredentialSource:
    """Resolve current secret material at each provider attempt."""

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Postmark credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared project settings and resolve provider credentials per call."""
    if project is None:
        raise ValueError("Postmark connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Postmark project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Postmark project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.postmark.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Postmark configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Postmark configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Postmark configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Postmark configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
