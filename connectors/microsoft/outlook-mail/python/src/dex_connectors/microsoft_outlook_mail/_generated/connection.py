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
from .definitions import CONNECTOR_ID, PROVIDER, GET_MESSAGE_DEFINITION, MOVE_MESSAGE_DEFINITION, REPLY_TO_MESSAGE_DEFINITION, SEARCH_MESSAGES_DEFINITION, SEND_MESSAGE_DEFINITION, SET_MESSAGE_FLAGS_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import GetMessageInput, Message, MessageFlags, MoveMessageInput, MovedMessage, ReplyToMessageInput, SearchMessagesInput, SearchMessagesOutput, SendMessageInput, SentMessage, SetMessageFlagsInput

class Connection:

    __slots__ = ('_search_messages', '_get_message', '_send_message', '_reply_to_message', '_move_message', '_set_message_flags', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_messages: Query[SearchMessagesInput, SearchMessagesOutput],
        get_message: Query[GetMessageInput, Message],
        send_message: Mutation[SendMessageInput, SentMessage],
        reply_to_message: Mutation[ReplyToMessageInput, SentMessage],
        move_message: Mutation[MoveMessageInput, MovedMessage],
        set_message_flags: Mutation[SetMessageFlagsInput, MessageFlags],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or search_messages is None or get_message is None or send_message is None or reply_to_message is None or move_message is None or set_message_flags is None:
            raise ValueError("Microsoft Outlook Mail connection provider and operations are required")
        if search_messages.definition().operation != SEARCH_MESSAGES_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Mail connection operation identity differs")
        if get_message.definition().operation != GET_MESSAGE_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Mail connection operation identity differs")
        if send_message.definition().operation != SEND_MESSAGE_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Mail connection operation identity differs")
        if reply_to_message.definition().operation != REPLY_TO_MESSAGE_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Mail connection operation identity differs")
        if move_message.definition().operation != MOVE_MESSAGE_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Mail connection operation identity differs")
        if set_message_flags.definition().operation != SET_MESSAGE_FLAGS_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Mail connection operation identity differs")
        self.reference = reference
        self._search_messages = search_messages
        self._get_message = get_message
        self._send_message = send_message
        self._reply_to_message = reply_to_message
        self._move_message = move_message
        self._set_message_flags = set_message_flags
        self._client = client

    def search_messages(self) -> Query[SearchMessagesInput, SearchMessagesOutput]:
        return self._search_messages

    def get_message(self) -> Query[GetMessageInput, Message]:
        return self._get_message

    def send_message(self) -> Mutation[SendMessageInput, SentMessage]:
        return self._send_message

    def reply_to_message(self) -> Mutation[ReplyToMessageInput, SentMessage]:
        return self._reply_to_message

    def move_message(self) -> Mutation[MoveMessageInput, MovedMessage]:
        return self._move_message

    def set_message_flags(self) -> Mutation[SetMessageFlagsInput, MessageFlags]:
        return self._set_message_flags

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "microsoft_outlook_mail.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Microsoft Outlook Mail connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.microsoft_outlook_mail.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Microsoft Outlook Mail client is required")
    return Connection(reference, client.search_messages(), client.get_message(), client.send_message(), client.reply_to_message(), client.move_message(), client.set_message_flags(), client)

setattr(new_connection, '__module__', 'dex_connectors.microsoft_outlook_mail.connection')


def new_connection_with_operations(
    search_messages: Query[SearchMessagesInput, SearchMessagesOutput],
    get_message: Query[GetMessageInput, Message],
    send_message: Mutation[SendMessageInput, SentMessage],
    reply_to_message: Mutation[ReplyToMessageInput, SentMessage],
    move_message: Mutation[MoveMessageInput, MovedMessage],
    set_message_flags: Mutation[SetMessageFlagsInput, MessageFlags],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_messages, get_message, send_message, reply_to_message, move_message, set_message_flags)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.microsoft_outlook_mail.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Microsoft Outlook Mail stored credential does not match the connector credential fields")
    if set(material) - set(('client_id', 'client_secret', 'access_token', 'refresh_token', 'tenant_id', 'auth_method')):
        raise ValueError("Microsoft Outlook Mail stored credential does not match the connector credential fields")
    is_shared = shared
    client_id = material.get("client_id")
    if client_id is not None and not (isinstance(client_id, str)):
        raise ValueError("Microsoft Outlook Mail credential client_id has an invalid type")
    client_secret = material.get("client_secret")
    if client_secret is not None and not (isinstance(client_secret, str)):
        raise ValueError("Microsoft Outlook Mail credential client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Microsoft Outlook Mail credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Microsoft Outlook Mail credential refresh_token has an invalid type")
    tenant_id = material.get("tenant_id")
    if tenant_id is not None and not (isinstance(tenant_id, str)):
        raise ValueError("Microsoft Outlook Mail credential tenant_id has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Microsoft Outlook Mail credential auth_method is invalid")
    method = selected_method or "microsoft-oauth"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "microsoft-oauth":
        if not client_id:
            raise ValueError("Microsoft Outlook Mail credential client_id is required")
        if client_id:
            values["client_id"] = client_id
        if not client_secret and not is_shared:
            raise ValueError("Microsoft Outlook Mail credential client_secret is required")
        if client_secret:
            values["client_secret"] = SecretString(client_secret)
        if not access_token:
            raise ValueError("Microsoft Outlook Mail credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Microsoft Outlook Mail credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    elif method == "app-only":
        if not tenant_id:
            raise ValueError("Microsoft Outlook Mail credential tenant_id is required")
        if tenant_id:
            values["tenant_id"] = tenant_id
        if not client_id:
            raise ValueError("Microsoft Outlook Mail credential client_id is required")
        if client_id:
            values["client_id"] = client_id
        if not client_secret and not is_shared:
            raise ValueError("Microsoft Outlook Mail credential client_secret is required")
        if client_secret:
            values["client_secret"] = SecretString(client_secret)
        if not access_token and is_shared:
            raise ValueError("Microsoft Outlook Mail credential access_token is required")
        values["access_token"] = SecretString(access_token or '')
    else:
        raise ValueError("Microsoft Outlook Mail credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "microsoft-oauth"
    material['auth_method'] = method
    if method == "microsoft-oauth":
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if (credentials.client_secret.reveal() if credentials.client_secret is not None else ''):
            material["client_secret"] = (credentials.client_secret.reveal() if credentials.client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    elif method == "app-only":
        if credentials.tenant_id:
            material["tenant_id"] = credentials.tenant_id
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if (credentials.client_secret.reveal() if credentials.client_secret is not None else ''):
            material["client_secret"] = (credentials.client_secret.reveal() if credentials.client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
    else:
        raise ValueError("Microsoft Outlook Mail credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Microsoft Outlook Mail", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Microsoft Outlook Mail connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Microsoft Outlook Mail project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Microsoft Outlook Mail project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.microsoft_outlook_mail.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "mailbox"}:
        raise ValueError("Microsoft Outlook Mail configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Microsoft Outlook Mail configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "mailbox" in value:
        mailbox = value["mailbox"]
        if not (isinstance(mailbox, str)):
            raise ValueError("Microsoft Outlook Mail configuration mailbox has an invalid type")
        config_arguments['mailbox'] = mailbox
    else:
        config_arguments['mailbox'] = ''
    return Config(**config_arguments)
