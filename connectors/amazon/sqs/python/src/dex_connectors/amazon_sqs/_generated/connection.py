# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CHANGE_MESSAGE_VISIBILITY_DEFINITION, DELETE_MESSAGE_DEFINITION, RECEIVE_MESSAGES_DEFINITION
from ..models import ChangeMessageVisibilityInput, ChangedMessageVisibility, DeleteMessageInput, DeletedMessage, ReceiveMessagesInput, ReceivedMessages

class Connection:

    __slots__ = ('_receive_messages', '_delete_message', '_change_message_visibility', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        receive_messages: Mutation[ReceiveMessagesInput, ReceivedMessages],
        delete_message: Mutation[DeleteMessageInput, DeletedMessage],
        change_message_visibility: Mutation[ChangeMessageVisibilityInput, ChangedMessageVisibility],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or receive_messages is None or delete_message is None or change_message_visibility is None:
            raise ValueError("Amazon SQS connection provider and operations are required")
        if receive_messages.definition().operation != RECEIVE_MESSAGES_DEFINITION.operation:
            raise ValueError("Amazon SQS connection operation identity differs")
        if delete_message.definition().operation != DELETE_MESSAGE_DEFINITION.operation:
            raise ValueError("Amazon SQS connection operation identity differs")
        if change_message_visibility.definition().operation != CHANGE_MESSAGE_VISIBILITY_DEFINITION.operation:
            raise ValueError("Amazon SQS connection operation identity differs")
        self.reference = reference
        self._receive_messages = receive_messages
        self._delete_message = delete_message
        self._change_message_visibility = change_message_visibility
        self._client = client

    def receive_messages(self) -> Mutation[ReceiveMessagesInput, ReceivedMessages]:
        return self._receive_messages

    def delete_message(self) -> Mutation[DeleteMessageInput, DeletedMessage]:
        return self._delete_message

    def change_message_visibility(self) -> Mutation[ChangeMessageVisibilityInput, ChangedMessageVisibility]:
        return self._change_message_visibility

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "amazon_sqs.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Amazon SQS connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.amazon_sqs.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Amazon SQS client is required")
    return Connection(reference, client.receive_messages(), client.delete_message(), client.change_message_visibility(), client)

setattr(new_connection, '__module__', 'dex_connectors.amazon_sqs.connection')


def new_connection_with_operations(
    receive_messages: Mutation[ReceiveMessagesInput, ReceivedMessages],
    delete_message: Mutation[DeleteMessageInput, DeletedMessage],
    change_message_visibility: Mutation[ChangeMessageVisibilityInput, ChangedMessageVisibility],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, receive_messages, delete_message, change_message_visibility)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.amazon_sqs.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        access_key_id = material.get("access_key_id")
        if not isinstance(access_key_id, str) or not access_key_id:
            raise ValueError("Amazon SQS credential access_key_id is required")
        secret_access_key = material.get("secret_access_key")
        if not isinstance(secret_access_key, str) or not secret_access_key:
            raise ValueError("Amazon SQS credential secret_access_key is required")
        session_token = material.get("session_token")
        if session_token is None:
            session_token = ''
        if not isinstance(session_token, str):
            raise ValueError("Amazon SQS credential session_token has an invalid type")
        return Credentials(SecretString(access_key_id), SecretString(secret_access_key), SecretString(session_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Amazon SQS connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Amazon SQS project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Amazon SQS project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.amazon_sqs.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"region", "endpoint", "defaultQueueUrl", "maxMessagesPerReceive", "receiveWaitSeconds", "maxResponseBytes"}:
        raise ValueError("Amazon SQS configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "region" in value and value["region"] != '':
        region = value["region"]
        if not (isinstance(region, str)):
            raise ValueError("Amazon SQS configuration region has an invalid type")
        config_arguments['region'] = region
    if "endpoint" in value and value["endpoint"] != '':
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Amazon SQS configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    else:
        config_arguments['endpoint'] = ''
    if "defaultQueueUrl" in value and value["defaultQueueUrl"] != '':
        default_queue_url = value["defaultQueueUrl"]
        if not (isinstance(default_queue_url, str)):
            raise ValueError("Amazon SQS configuration defaultQueueUrl has an invalid type")
        config_arguments['default_queue_url'] = default_queue_url
    else:
        config_arguments['default_queue_url'] = ''
    if "maxMessagesPerReceive" in value and value["maxMessagesPerReceive"] != '':
        max_messages_per_receive = value["maxMessagesPerReceive"]
        if not (type(max_messages_per_receive) is int):
            raise ValueError("Amazon SQS configuration maxMessagesPerReceive has an invalid type")
        config_arguments['max_messages_per_receive'] = max_messages_per_receive
    if "receiveWaitSeconds" in value and value["receiveWaitSeconds"] != '':
        receive_wait_seconds = value["receiveWaitSeconds"]
        if not (type(receive_wait_seconds) is int):
            raise ValueError("Amazon SQS configuration receiveWaitSeconds has an invalid type")
        config_arguments['receive_wait_seconds'] = receive_wait_seconds
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Amazon SQS configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
