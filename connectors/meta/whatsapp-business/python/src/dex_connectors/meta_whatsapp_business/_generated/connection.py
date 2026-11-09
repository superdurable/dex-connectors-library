# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_PHONE_NUMBERS_DEFINITION, SEND_TEMPLATE_MESSAGE_DEFINITION, SEND_TEXT_MESSAGE_DEFINITION
from ..models import ListPhoneNumbersInput, PhoneNumbersPage, SendTemplateMessageInput, SendTextMessageInput, SentMessage

class Connection:

    __slots__ = ('_list_phone_numbers', '_send_text_message', '_send_template_message', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_phone_numbers: Query[ListPhoneNumbersInput, PhoneNumbersPage],
        send_text_message: Mutation[SendTextMessageInput, SentMessage],
        send_template_message: Mutation[SendTemplateMessageInput, SentMessage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_phone_numbers is None or send_text_message is None or send_template_message is None:
            raise ValueError("WhatsApp Business Platform connection provider and operations are required")
        if list_phone_numbers.definition().operation != LIST_PHONE_NUMBERS_DEFINITION.operation:
            raise ValueError("WhatsApp Business Platform connection operation identity differs")
        if send_text_message.definition().operation != SEND_TEXT_MESSAGE_DEFINITION.operation:
            raise ValueError("WhatsApp Business Platform connection operation identity differs")
        if send_template_message.definition().operation != SEND_TEMPLATE_MESSAGE_DEFINITION.operation:
            raise ValueError("WhatsApp Business Platform connection operation identity differs")
        self.reference = reference
        self._list_phone_numbers = list_phone_numbers
        self._send_text_message = send_text_message
        self._send_template_message = send_template_message
        self._client = client

    def list_phone_numbers(self) -> Query[ListPhoneNumbersInput, PhoneNumbersPage]:
        return self._list_phone_numbers

    def send_text_message(self) -> Mutation[SendTextMessageInput, SentMessage]:
        return self._send_text_message

    def send_template_message(self) -> Mutation[SendTemplateMessageInput, SentMessage]:
        return self._send_template_message

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "meta_whatsapp_business.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("WhatsApp Business Platform connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.meta_whatsapp_business.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("WhatsApp Business Platform client is required")
    return Connection(reference, client.list_phone_numbers(), client.send_text_message(), client.send_template_message(), client)

setattr(new_connection, '__module__', 'dex_connectors.meta_whatsapp_business.connection')


def new_connection_with_operations(
    list_phone_numbers: Query[ListPhoneNumbersInput, PhoneNumbersPage],
    send_text_message: Mutation[SendTextMessageInput, SentMessage],
    send_template_message: Mutation[SendTemplateMessageInput, SentMessage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_phone_numbers, send_text_message, send_template_message)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.meta_whatsapp_business.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("WhatsApp Business Platform stored credential does not match the connector credential fields")
        if set(material) - set(('access_token',)):
            raise ValueError("WhatsApp Business Platform stored credential does not match the connector credential fields")
        access_token = material.get("access_token")
        if access_token is not None and not (isinstance(access_token, str)):
            raise ValueError("WhatsApp Business Platform credential access_token has an invalid type")
        if not access_token:
            raise ValueError("WhatsApp Business Platform credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("WhatsApp Business Platform connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("WhatsApp Business Platform project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("WhatsApp Business Platform project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.meta_whatsapp_business.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("WhatsApp Business Platform configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("WhatsApp Business Platform configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("WhatsApp Business Platform configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("WhatsApp Business Platform configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
