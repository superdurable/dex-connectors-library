# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, FIND_CUSTOMER_BY_EMAIL_DEFINITION, GET_CONVERSATION_DEFINITION, REPLY_TO_CONVERSATION_DEFINITION, SEARCH_CONVERSATIONS_DEFINITION, UPDATE_CONVERSATION_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import ConversationDetails, ConversationReply, CustomerMatches, FindCustomerByEmailInput, GetConversationInput, ReplyToConversationInput, SearchConversationsInput, SearchConversationsOutput, UpdateConversationInput, UpdateConversationOutput

class Connection:

    __slots__ = ('_search_conversations', '_get_conversation', '_reply_to_conversation', '_update_conversation', '_find_customer_by_email', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_conversations: Query[SearchConversationsInput, SearchConversationsOutput],
        get_conversation: Query[GetConversationInput, ConversationDetails],
        reply_to_conversation: Mutation[ReplyToConversationInput, ConversationReply],
        update_conversation: Mutation[UpdateConversationInput, UpdateConversationOutput],
        find_customer_by_email: Query[FindCustomerByEmailInput, CustomerMatches],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search_conversations is None or get_conversation is None or reply_to_conversation is None or update_conversation is None or find_customer_by_email is None:
            raise ValueError("Help Scout connection provider and operations are required")
        if search_conversations.definition().operation != SEARCH_CONVERSATIONS_DEFINITION.operation:
            raise ValueError("Help Scout connection operation identity differs")
        if get_conversation.definition().operation != GET_CONVERSATION_DEFINITION.operation:
            raise ValueError("Help Scout connection operation identity differs")
        if reply_to_conversation.definition().operation != REPLY_TO_CONVERSATION_DEFINITION.operation:
            raise ValueError("Help Scout connection operation identity differs")
        if update_conversation.definition().operation != UPDATE_CONVERSATION_DEFINITION.operation:
            raise ValueError("Help Scout connection operation identity differs")
        if find_customer_by_email.definition().operation != FIND_CUSTOMER_BY_EMAIL_DEFINITION.operation:
            raise ValueError("Help Scout connection operation identity differs")
        self.reference = reference
        self._search_conversations = search_conversations
        self._get_conversation = get_conversation
        self._reply_to_conversation = reply_to_conversation
        self._update_conversation = update_conversation
        self._find_customer_by_email = find_customer_by_email
        self._client = client

    def search_conversations(self) -> Query[SearchConversationsInput, SearchConversationsOutput]:
        return self._search_conversations

    def get_conversation(self) -> Query[GetConversationInput, ConversationDetails]:
        return self._get_conversation

    def reply_to_conversation(self) -> Mutation[ReplyToConversationInput, ConversationReply]:
        return self._reply_to_conversation

    def update_conversation(self) -> Mutation[UpdateConversationInput, UpdateConversationOutput]:
        return self._update_conversation

    def find_customer_by_email(self) -> Query[FindCustomerByEmailInput, CustomerMatches]:
        return self._find_customer_by_email

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "helpscout.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Help Scout connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.helpscout.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Help Scout client is required")
    return Connection(reference, client.search_conversations(), client.get_conversation(), client.reply_to_conversation(), client.update_conversation(), client.find_customer_by_email(), client)

setattr(new_connection, '__module__', 'dex_connectors.helpscout.connection')


def new_connection_with_operations(
    search_conversations: Query[SearchConversationsInput, SearchConversationsOutput],
    get_conversation: Query[GetConversationInput, ConversationDetails],
    reply_to_conversation: Mutation[ReplyToConversationInput, ConversationReply],
    update_conversation: Mutation[UpdateConversationInput, UpdateConversationOutput],
    find_customer_by_email: Query[FindCustomerByEmailInput, CustomerMatches],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_conversations, get_conversation, reply_to_conversation, update_conversation, find_customer_by_email)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.helpscout.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Help Scout stored credential does not match the connector credential fields")
    if set(material) - set(('app_id', 'app_secret', 'access_token', 'webhook_secret')):
        raise ValueError("Help Scout stored credential does not match the connector credential fields")
    is_shared = shared
    app_id = material.get("app_id")
    if app_id is not None and not (isinstance(app_id, str)):
        raise ValueError("Help Scout credential app_id has an invalid type")
    app_secret = material.get("app_secret")
    if app_secret is not None and not (isinstance(app_secret, str)):
        raise ValueError("Help Scout credential app_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Help Scout credential access_token has an invalid type")
    webhook_secret = material.get("webhook_secret")
    if webhook_secret is not None and not (isinstance(webhook_secret, str)):
        raise ValueError("Help Scout credential webhook_secret has an invalid type")
    if not app_id:
        raise ValueError("Help Scout credential app_id is required")
    if not app_secret and not is_shared:
        raise ValueError("Help Scout credential app_secret is required")
    if not access_token and is_shared:
        raise ValueError("Help Scout credential access_token is required")
    return Credentials(app_id, SecretString(app_secret or ''), SecretString(access_token or ''), SecretString(webhook_secret or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.app_id:
        material["app_id"] = credentials.app_id
    if credentials.app_secret.reveal():
        material["app_secret"] = credentials.app_secret.reveal()
    if credentials.access_token.reveal():
        material["access_token"] = credentials.access_token.reveal()
    if credentials.webhook_secret.reveal():
        material["webhook_secret"] = credentials.webhook_secret.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Help Scout", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

    async def resolve(self, call: Call) -> Credentials:
        return await self.provider.resolve_with_refresh(call, self.driver)

    async def resolve_with_refresh(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_with_refresh(call, driver or self.driver)

    async def resolve_after_rejection(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_after_rejection(call, driver or self.driver)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Help Scout connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Help Scout project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Help Scout project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.helpscout.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "webhookMaxBodyBytes"}:
        raise ValueError("Help Scout configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Help Scout configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "webhookMaxBodyBytes" in value and value["webhookMaxBodyBytes"] != '':
        webhook_max_body_bytes = value["webhookMaxBodyBytes"]
        if not (type(webhook_max_body_bytes) is int):
            raise ValueError("Help Scout configuration webhookMaxBodyBytes has an invalid type")
        config_arguments['webhook_max_body_bytes'] = webhook_max_body_bytes
    return Config(**config_arguments)
