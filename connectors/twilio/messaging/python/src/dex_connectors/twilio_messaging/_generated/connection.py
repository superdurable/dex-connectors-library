# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_MESSAGE_DEFINITION, SEND_MESSAGE_DEFINITION
from ..models import GetMessageInput, Message, SendMessageInput

class Connection:

    __slots__ = ('_send_message', '_get_message', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        send_message: Mutation[SendMessageInput, Message],
        get_message: Query[GetMessageInput, Message],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or send_message is None or get_message is None:
            raise ValueError("Twilio Messaging connection provider and operations are required")
        if send_message.definition().operation != SEND_MESSAGE_DEFINITION.operation:
            raise ValueError("Twilio Messaging connection operation identity differs")
        if get_message.definition().operation != GET_MESSAGE_DEFINITION.operation:
            raise ValueError("Twilio Messaging connection operation identity differs")
        self.reference = reference
        self._send_message = send_message
        self._get_message = get_message
        self._client = client

    def send_message(self) -> Mutation[SendMessageInput, Message]:
        return self._send_message

    def get_message(self) -> Query[GetMessageInput, Message]:
        return self._get_message

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "twilio_messaging.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Twilio Messaging connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.twilio_messaging.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Twilio Messaging client is required")
    return Connection(reference, client.send_message(), client.get_message(), client)

setattr(new_connection, '__module__', 'dex_connectors.twilio_messaging.connection')


def new_connection_with_operations(
    send_message: Mutation[SendMessageInput, Message],
    get_message: Query[GetMessageInput, Message],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, send_message, get_message)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.twilio_messaging.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Twilio Messaging stored credential does not match the connector credential fields")
        if set(material) - set(('auth_token', 'api_key_sid', 'api_key_secret', 'auth_method')):
            raise ValueError("Twilio Messaging stored credential does not match the connector credential fields")
        auth_token = material.get("auth_token")
        if auth_token is not None and not (isinstance(auth_token, str)):
            raise ValueError("Twilio Messaging credential auth_token has an invalid type")
        api_key_sid = material.get("api_key_sid")
        if api_key_sid is not None and not (isinstance(api_key_sid, str)):
            raise ValueError("Twilio Messaging credential api_key_sid has an invalid type")
        api_key_secret = material.get("api_key_secret")
        if api_key_secret is not None and not (isinstance(api_key_secret, str)):
            raise ValueError("Twilio Messaging credential api_key_secret has an invalid type")
        selected_method = material.get('auth_method')
        if selected_method is None:
            selected_method = ''
        if not isinstance(selected_method, str):
            raise ValueError("Twilio Messaging credential auth_method is invalid")
        method = selected_method or "auth-token"
        values: dict[str, Any] = {'auth_method_id': method}
        if method == "auth-token":
            if not auth_token:
                raise ValueError("Twilio Messaging credential auth_token is required")
            if auth_token:
                values["auth_token"] = SecretString(auth_token)
        elif method == "api-key":
            if not api_key_sid:
                raise ValueError("Twilio Messaging credential api_key_sid is required")
            if api_key_sid:
                values["api_key_sid"] = api_key_sid
            if not api_key_secret:
                raise ValueError("Twilio Messaging credential api_key_secret is required")
            if api_key_secret:
                values["api_key_secret"] = SecretString(api_key_secret)
        else:
            raise ValueError("Twilio Messaging credential auth_method is invalid")
        return Credentials(**values)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Twilio Messaging connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Twilio Messaging project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Twilio Messaging project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.twilio_messaging.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"accountSid", "defaultSender", "endpoint", "maxResponseBytes"}:
        raise ValueError("Twilio Messaging configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "accountSid" in value:
        account_sid = value["accountSid"]
        if not (isinstance(account_sid, str)):
            raise ValueError("Twilio Messaging configuration accountSid has an invalid type")
        config_arguments['account_sid'] = account_sid
    else:
        raise ValueError("Twilio Messaging configuration accountSid is required")
    if "defaultSender" in value:
        default_sender = value["defaultSender"]
        if not (isinstance(default_sender, str)):
            raise ValueError("Twilio Messaging configuration defaultSender has an invalid type")
        config_arguments['default_sender'] = default_sender
    else:
        config_arguments['default_sender'] = ''
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Twilio Messaging configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Twilio Messaging configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
