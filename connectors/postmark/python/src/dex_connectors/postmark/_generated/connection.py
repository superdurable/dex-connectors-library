# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_MESSAGE_DETAILS_DEFINITION, LIST_OUTBOUND_MESSAGES_DEFINITION
from ..models import GetMessageDetailsInput, ListOutboundMessagesInput, MessageDetails, MessagePage

class Connection:

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
        return self._list_outbound_messages

    def get_message_details(self) -> Query[GetMessageDetailsInput, MessageDetails]:
        return self._get_message_details

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "postmark.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Postmark connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.postmark.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Postmark client is required")
    return Connection(reference, client.list_outbound_messages(), client.get_message_details(), client)

setattr(new_connection, '__module__', 'dex_connectors.postmark.connection')


def new_connection_with_operations(
    list_outbound_messages: Query[ListOutboundMessagesInput, MessagePage],
    get_message_details: Query[GetMessageDetailsInput, MessageDetails],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_outbound_messages, get_message_details)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.postmark.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Postmark stored credential does not match the connector credential fields")
        if set(material) - set(('api_key',)):
            raise ValueError("Postmark stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Postmark credential api_key has an invalid type")
        if not api_key:
            raise ValueError("Postmark credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
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
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Postmark configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Postmark configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Postmark configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
