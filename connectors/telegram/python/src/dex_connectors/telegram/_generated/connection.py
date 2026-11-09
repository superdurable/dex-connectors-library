# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_CHAT_DEFINITION, SEND_MESSAGE_DEFINITION
from ..models import Chat, GetChatInput, SendMessageInput, SentMessage

class Connection:

    __slots__ = ('_send_message', '_get_chat', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        send_message: Mutation[SendMessageInput, SentMessage],
        get_chat: Query[GetChatInput, Chat],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or send_message is None or get_chat is None:
            raise ValueError("Telegram Bot connection provider and operations are required")
        if send_message.definition().operation != SEND_MESSAGE_DEFINITION.operation:
            raise ValueError("Telegram Bot connection operation identity differs")
        if get_chat.definition().operation != GET_CHAT_DEFINITION.operation:
            raise ValueError("Telegram Bot connection operation identity differs")
        self.reference = reference
        self._send_message = send_message
        self._get_chat = get_chat
        self._client = client

    def send_message(self) -> Mutation[SendMessageInput, SentMessage]:
        return self._send_message

    def get_chat(self) -> Query[GetChatInput, Chat]:
        return self._get_chat

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "telegram.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Telegram Bot connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.telegram.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Telegram Bot client is required")
    return Connection(reference, client.send_message(), client.get_chat(), client)

setattr(new_connection, '__module__', 'dex_connectors.telegram.connection')


def new_connection_with_operations(
    send_message: Mutation[SendMessageInput, SentMessage],
    get_chat: Query[GetChatInput, Chat],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, send_message, get_chat)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.telegram.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Telegram Bot stored credential does not match the connector credential fields")
        if set(material) - set(('bot_token',)):
            raise ValueError("Telegram Bot stored credential does not match the connector credential fields")
        bot_token = material.get("bot_token")
        if bot_token is not None and not (isinstance(bot_token, str)):
            raise ValueError("Telegram Bot credential bot_token has an invalid type")
        if not bot_token:
            raise ValueError("Telegram Bot credential bot_token is required")
        return Credentials(SecretString(bot_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Telegram Bot connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Telegram Bot project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Telegram Bot project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.telegram.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Telegram Bot configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Telegram Bot configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Telegram Bot configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Telegram Bot configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
