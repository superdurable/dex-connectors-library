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
from .definitions import CONNECTOR_ID, GET_THREAD_REPLY_DEFINITION, LIST_THREAD_MESSAGES_DEFINITION, POST_CHANNEL_MESSAGE_DEFINITION, POST_THREAD_REPLY_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import GetThreadReplyInput, GetThreadReplyOutput, ListThreadMessagesInput, ListThreadMessagesOutput, PostChannelMessageInput, PostMessageOutput, PostThreadReplyInput

class Connection:

    __slots__ = ('_list_thread_messages', '_get_thread_reply', '_post_channel_message', '_post_thread_reply', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_thread_messages: Query[ListThreadMessagesInput, ListThreadMessagesOutput],
        get_thread_reply: Query[GetThreadReplyInput, GetThreadReplyOutput],
        post_channel_message: Mutation[PostChannelMessageInput, PostMessageOutput],
        post_thread_reply: Mutation[PostThreadReplyInput, PostMessageOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_thread_messages is None or get_thread_reply is None or post_channel_message is None or post_thread_reply is None:
            raise ValueError("Slack connection provider and operations are required")
        if list_thread_messages.definition().operation != LIST_THREAD_MESSAGES_DEFINITION.operation:
            raise ValueError("Slack connection operation identity differs")
        if get_thread_reply.definition().operation != GET_THREAD_REPLY_DEFINITION.operation:
            raise ValueError("Slack connection operation identity differs")
        if post_channel_message.definition().operation != POST_CHANNEL_MESSAGE_DEFINITION.operation:
            raise ValueError("Slack connection operation identity differs")
        if post_thread_reply.definition().operation != POST_THREAD_REPLY_DEFINITION.operation:
            raise ValueError("Slack connection operation identity differs")
        self.reference = reference
        self._list_thread_messages = list_thread_messages
        self._get_thread_reply = get_thread_reply
        self._post_channel_message = post_channel_message
        self._post_thread_reply = post_thread_reply
        self._client = client

    def list_thread_messages(self) -> Query[ListThreadMessagesInput, ListThreadMessagesOutput]:
        return self._list_thread_messages

    def get_thread_reply(self) -> Query[GetThreadReplyInput, GetThreadReplyOutput]:
        return self._get_thread_reply

    def post_channel_message(self) -> Mutation[PostChannelMessageInput, PostMessageOutput]:
        return self._post_channel_message

    def post_thread_reply(self) -> Mutation[PostThreadReplyInput, PostMessageOutput]:
        return self._post_thread_reply

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "slack.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Slack connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.slack.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Slack client is required")
    return Connection(reference, client.list_thread_messages(), client.get_thread_reply(), client.post_channel_message(), client.post_thread_reply(), client)

setattr(new_connection, '__module__', 'dex_connectors.slack.connection')


def new_connection_with_operations(
    list_thread_messages: Query[ListThreadMessagesInput, ListThreadMessagesOutput],
    get_thread_reply: Query[GetThreadReplyInput, GetThreadReplyOutput],
    post_channel_message: Mutation[PostChannelMessageInput, PostMessageOutput],
    post_thread_reply: Mutation[PostThreadReplyInput, PostMessageOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_thread_messages, get_thread_reply, post_channel_message, post_thread_reply)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.slack.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Slack stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'bot_token', 'bot_refresh_token', 'user_token', 'user_refresh_token', 'app_token')):
        raise ValueError("Slack stored credential does not match the connector credential fields")
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Slack credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Slack credential oauth_client_secret has an invalid type")
    bot_token = material.get("bot_token")
    if bot_token is not None and not (isinstance(bot_token, str)):
        raise ValueError("Slack credential bot_token has an invalid type")
    bot_refresh_token = material.get("bot_refresh_token")
    if bot_refresh_token is not None and not (isinstance(bot_refresh_token, str)):
        raise ValueError("Slack credential bot_refresh_token has an invalid type")
    user_token = material.get("user_token")
    if user_token is not None and not (isinstance(user_token, str)):
        raise ValueError("Slack credential user_token has an invalid type")
    user_refresh_token = material.get("user_refresh_token")
    if user_refresh_token is not None and not (isinstance(user_refresh_token, str)):
        raise ValueError("Slack credential user_refresh_token has an invalid type")
    app_token = material.get("app_token")
    if app_token is not None and not (isinstance(app_token, str)):
        raise ValueError("Slack credential app_token has an invalid type")
    if not bot_token:
        raise ValueError("Slack credential bot_token is required")
    if not user_token:
        raise ValueError("Slack credential user_token is required")
    return Credentials((oauth_client_id or ''), SecretString(oauth_client_secret or ''), SecretString(bot_token), SecretString(bot_refresh_token or ''), SecretString(user_token), SecretString(user_refresh_token or ''), SecretString(app_token or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.oauth_client_id:
        material["oauth_client_id"] = credentials.oauth_client_id
    if credentials.oauth_client_secret.reveal():
        material["oauth_client_secret"] = credentials.oauth_client_secret.reveal()
    if credentials.bot_token.reveal():
        material["bot_token"] = credentials.bot_token.reveal()
    if credentials.bot_refresh_token.reveal():
        material["bot_refresh_token"] = credentials.bot_refresh_token.reveal()
    if credentials.user_token.reveal():
        material["user_token"] = credentials.user_token.reveal()
    if credentials.user_refresh_token.reveal():
        material["user_refresh_token"] = credentials.user_refresh_token.reveal()
    if credentials.app_token.reveal():
        material["app_token"] = credentials.app_token.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Slack", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Slack connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Slack project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Slack project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.slack.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes", "maxMessageCharacters"}:
        raise ValueError("Slack configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value and value["endpoint"] != '':
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Slack configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Slack configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxMessageCharacters" in value and value["maxMessageCharacters"] != '':
        max_message_characters = value["maxMessageCharacters"]
        if not (type(max_message_characters) is int):
            raise ValueError("Slack configuration maxMessageCharacters has an invalid type")
        config_arguments['max_message_characters'] = max_message_characters
    return Config(**config_arguments)
