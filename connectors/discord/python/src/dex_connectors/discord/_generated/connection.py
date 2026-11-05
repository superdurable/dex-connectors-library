# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, ADD_REACTION_DEFINITION, CREATE_MESSAGE_DEFINITION, GET_CHANNEL_DEFINITION, LIST_CHANNEL_MESSAGES_DEFINITION
from ..models import AddReactionInput, AddedReaction, Channel, CreateMessageInput, GetChannelInput, ListChannelMessagesInput, Message, MessagePage

class Connection:

    __slots__ = ('_get_channel', '_list_channel_messages', '_create_message', '_add_reaction', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_channel: Query[GetChannelInput, Channel],
        list_channel_messages: Query[ListChannelMessagesInput, MessagePage],
        create_message: Mutation[CreateMessageInput, Message],
        add_reaction: Mutation[AddReactionInput, AddedReaction],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_channel is None or list_channel_messages is None or create_message is None or add_reaction is None:
            raise ValueError("Discord connection provider and operations are required")
        if get_channel.definition().operation != GET_CHANNEL_DEFINITION.operation:
            raise ValueError("Discord connection operation identity differs")
        if list_channel_messages.definition().operation != LIST_CHANNEL_MESSAGES_DEFINITION.operation:
            raise ValueError("Discord connection operation identity differs")
        if create_message.definition().operation != CREATE_MESSAGE_DEFINITION.operation:
            raise ValueError("Discord connection operation identity differs")
        if add_reaction.definition().operation != ADD_REACTION_DEFINITION.operation:
            raise ValueError("Discord connection operation identity differs")
        self.reference = reference
        self._get_channel = get_channel
        self._list_channel_messages = list_channel_messages
        self._create_message = create_message
        self._add_reaction = add_reaction
        self._client = client

    def get_channel(self) -> Query[GetChannelInput, Channel]:
        return self._get_channel

    def list_channel_messages(self) -> Query[ListChannelMessagesInput, MessagePage]:
        return self._list_channel_messages

    def create_message(self) -> Mutation[CreateMessageInput, Message]:
        return self._create_message

    def add_reaction(self) -> Mutation[AddReactionInput, AddedReaction]:
        return self._add_reaction

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "discord.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Discord connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.discord.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Discord client is required")
    return Connection(reference, client.get_channel(), client.list_channel_messages(), client.create_message(), client.add_reaction(), client)

setattr(new_connection, '__module__', 'dex_connectors.discord.connection')


def new_connection_with_operations(
    get_channel: Query[GetChannelInput, Channel],
    list_channel_messages: Query[ListChannelMessagesInput, MessagePage],
    create_message: Mutation[CreateMessageInput, Message],
    add_reaction: Mutation[AddReactionInput, AddedReaction],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_channel, list_channel_messages, create_message, add_reaction)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.discord.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Discord stored credential does not match the connector credential fields")
        if set(material) - set(('bot_token',)):
            raise ValueError("Discord stored credential does not match the connector credential fields")
        bot_token = material.get("bot_token")
        if bot_token is not None and not (isinstance(bot_token, str)):
            raise ValueError("Discord credential bot_token has an invalid type")
        if not bot_token:
            raise ValueError("Discord credential bot_token is required")
        return Credentials(SecretString(bot_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Discord connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Discord project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Discord project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.discord.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes"}:
        raise ValueError("Discord configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Discord configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Discord configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
