# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, FIND_CONTACT_BY_EMAIL_DEFINITION, GET_CONVERSATION_DEFINITION, REPLY_TO_CONVERSATION_DEFINITION, SEARCH_CONVERSATIONS_DEFINITION, UPDATE_CONVERSATION_STATE_DEFINITION
from ..models import ConversationDetails, FindContactByEmailInput, FindContactByEmailOutput, GetConversationInput, ReplyToConversationInput, ReplyToConversationOutput, SearchConversationsInput, SearchConversationsOutput, UpdateConversationStateInput, UpdateConversationStateOutput

class Connection:

    __slots__ = ('_search_conversations', '_get_conversation', '_reply_to_conversation', '_update_conversation_state', '_find_contact_by_email', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_conversations: Query[SearchConversationsInput, SearchConversationsOutput],
        get_conversation: Query[GetConversationInput, ConversationDetails],
        reply_to_conversation: Mutation[ReplyToConversationInput, ReplyToConversationOutput],
        update_conversation_state: Mutation[UpdateConversationStateInput, UpdateConversationStateOutput],
        find_contact_by_email: Query[FindContactByEmailInput, FindContactByEmailOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search_conversations is None or get_conversation is None or reply_to_conversation is None or update_conversation_state is None or find_contact_by_email is None:
            raise ValueError("Intercom connection provider and operations are required")
        if search_conversations.definition().operation != SEARCH_CONVERSATIONS_DEFINITION.operation:
            raise ValueError("Intercom connection operation identity differs")
        if get_conversation.definition().operation != GET_CONVERSATION_DEFINITION.operation:
            raise ValueError("Intercom connection operation identity differs")
        if reply_to_conversation.definition().operation != REPLY_TO_CONVERSATION_DEFINITION.operation:
            raise ValueError("Intercom connection operation identity differs")
        if update_conversation_state.definition().operation != UPDATE_CONVERSATION_STATE_DEFINITION.operation:
            raise ValueError("Intercom connection operation identity differs")
        if find_contact_by_email.definition().operation != FIND_CONTACT_BY_EMAIL_DEFINITION.operation:
            raise ValueError("Intercom connection operation identity differs")
        self.reference = reference
        self._search_conversations = search_conversations
        self._get_conversation = get_conversation
        self._reply_to_conversation = reply_to_conversation
        self._update_conversation_state = update_conversation_state
        self._find_contact_by_email = find_contact_by_email
        self._client = client

    def search_conversations(self) -> Query[SearchConversationsInput, SearchConversationsOutput]:
        return self._search_conversations

    def get_conversation(self) -> Query[GetConversationInput, ConversationDetails]:
        return self._get_conversation

    def reply_to_conversation(self) -> Mutation[ReplyToConversationInput, ReplyToConversationOutput]:
        return self._reply_to_conversation

    def update_conversation_state(self) -> Mutation[UpdateConversationStateInput, UpdateConversationStateOutput]:
        return self._update_conversation_state

    def find_contact_by_email(self) -> Query[FindContactByEmailInput, FindContactByEmailOutput]:
        return self._find_contact_by_email

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "intercom.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Intercom connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.intercom.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Intercom client is required")
    return Connection(reference, client.search_conversations(), client.get_conversation(), client.reply_to_conversation(), client.update_conversation_state(), client.find_contact_by_email(), client)

setattr(new_connection, '__module__', 'dex_connectors.intercom.connection')


def new_connection_with_operations(
    search_conversations: Query[SearchConversationsInput, SearchConversationsOutput],
    get_conversation: Query[GetConversationInput, ConversationDetails],
    reply_to_conversation: Mutation[ReplyToConversationInput, ReplyToConversationOutput],
    update_conversation_state: Mutation[UpdateConversationStateInput, UpdateConversationStateOutput],
    find_contact_by_email: Query[FindContactByEmailInput, FindContactByEmailOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_conversations, get_conversation, reply_to_conversation, update_conversation_state, find_contact_by_email)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.intercom.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        access_token = material.get("access_token")
        if not isinstance(access_token, str) or not access_token:
            raise ValueError("Intercom credential access_token is required")
        client_secret = material.get("client_secret")
        if client_secret is None:
            client_secret = ''
        if not isinstance(client_secret, str):
            raise ValueError("Intercom credential client_secret has an invalid type")
        return Credentials(SecretString(access_token), SecretString(client_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Intercom connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Intercom project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Intercom project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.intercom.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"region", "maxResponseBytes", "webhookMaxBodyBytes"}:
        raise ValueError("Intercom configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "region" in value and value["region"] != '':
        region = value["region"]
        if not (isinstance(region, str) and region in ('us', 'eu', 'au')):
            raise ValueError("Intercom configuration region has an invalid type")
        config_arguments['region'] = region
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Intercom configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "webhookMaxBodyBytes" in value and value["webhookMaxBodyBytes"] != '':
        webhook_max_body_bytes = value["webhookMaxBodyBytes"]
        if not (type(webhook_max_body_bytes) is int):
            raise ValueError("Intercom configuration webhookMaxBodyBytes has an invalid type")
        config_arguments['webhook_max_body_bytes'] = webhook_max_body_bytes
    return Config(**config_arguments)
