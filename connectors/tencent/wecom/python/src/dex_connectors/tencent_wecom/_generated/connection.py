# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, SEND_APPLICATION_MESSAGE_DEFINITION, SEND_GROUP_ROBOT_MESSAGE_DEFINITION
from ..models import SendApplicationMessageInput, SendGroupRobotMessageInput, SentApplicationMessage, SentGroupRobotMessage

class Connection:

    __slots__ = ('_send_group_robot_message', '_send_application_message', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        send_group_robot_message: Mutation[SendGroupRobotMessageInput, SentGroupRobotMessage],
        send_application_message: Mutation[SendApplicationMessageInput, SentApplicationMessage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or send_group_robot_message is None or send_application_message is None:
            raise ValueError("WeCom connection provider and operations are required")
        if send_group_robot_message.definition().operation != SEND_GROUP_ROBOT_MESSAGE_DEFINITION.operation:
            raise ValueError("WeCom connection operation identity differs")
        if send_application_message.definition().operation != SEND_APPLICATION_MESSAGE_DEFINITION.operation:
            raise ValueError("WeCom connection operation identity differs")
        self.reference = reference
        self._send_group_robot_message = send_group_robot_message
        self._send_application_message = send_application_message
        self._client = client

    def send_group_robot_message(self) -> Mutation[SendGroupRobotMessageInput, SentGroupRobotMessage]:
        return self._send_group_robot_message

    def send_application_message(self) -> Mutation[SendApplicationMessageInput, SentApplicationMessage]:
        return self._send_application_message

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "tencent_wecom.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("WeCom connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.tencent_wecom.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("WeCom client is required")
    return Connection(reference, client.send_group_robot_message(), client.send_application_message(), client)

setattr(new_connection, '__module__', 'dex_connectors.tencent_wecom.connection')


def new_connection_with_operations(
    send_group_robot_message: Mutation[SendGroupRobotMessageInput, SentGroupRobotMessage],
    send_application_message: Mutation[SendApplicationMessageInput, SentApplicationMessage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, send_group_robot_message, send_application_message)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.tencent_wecom.connection')


class CredentialMaterialError(ValueError):
    "Stored WeCom credential material fails the connector credential checks, so no retry can succeed."


def is_credential_material_error(error: BaseException) -> bool:
    """Report malformed stored credential material, also behind the SDK provider's decoding ValueError."""
    if isinstance(error, CredentialMaterialError):
        return True
    # Only the SDK decoding wrapper qualifies; other errors may be transient.
    return (
        type(error) is ValueError
        and str(error) in {'project credential decoding failed', 'shared project credential decoding failed'}
        and isinstance(error.__context__, CredentialMaterialError)
    )


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise CredentialMaterialError("WeCom stored credential does not match the connector credential fields")
        if set(material) - set(('webhook_key', 'corp_id', 'agent_id', 'corp_secret', 'auth_method')):
            raise CredentialMaterialError("WeCom stored credential does not match the connector credential fields")
        webhook_key = material.get("webhook_key")
        if webhook_key is not None and not (isinstance(webhook_key, str)):
            raise CredentialMaterialError("WeCom credential webhook_key has an invalid type")
        corp_id = material.get("corp_id")
        if corp_id is not None and not (isinstance(corp_id, str)):
            raise CredentialMaterialError("WeCom credential corp_id has an invalid type")
        agent_id = material.get("agent_id")
        if agent_id is not None and not (isinstance(agent_id, str)):
            raise CredentialMaterialError("WeCom credential agent_id has an invalid type")
        corp_secret = material.get("corp_secret")
        if corp_secret is not None and not (isinstance(corp_secret, str)):
            raise CredentialMaterialError("WeCom credential corp_secret has an invalid type")
        selected_method = material.get('auth_method')
        if selected_method is None:
            selected_method = ''
        if not isinstance(selected_method, str):
            raise CredentialMaterialError("WeCom credential auth_method is invalid")
        method = selected_method or "group-robot"
        values: dict[str, Any] = {'auth_method_id': method}
        if method == "group-robot":
            if not webhook_key:
                raise CredentialMaterialError("WeCom credential webhook_key is required")
            if webhook_key:
                values["webhook_key"] = SecretString(webhook_key)
        elif method == "application":
            if not corp_id:
                raise CredentialMaterialError("WeCom credential corp_id is required")
            if corp_id:
                values["corp_id"] = corp_id
            if not agent_id:
                raise CredentialMaterialError("WeCom credential agent_id is required")
            if agent_id:
                values["agent_id"] = agent_id
            if not corp_secret:
                raise CredentialMaterialError("WeCom credential corp_secret is required")
            if corp_secret:
                values["corp_secret"] = SecretString(corp_secret)
        else:
            raise CredentialMaterialError("WeCom credential auth_method is invalid")
        return Credentials(**values)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("WeCom connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("WeCom project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("WeCom project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.tencent_wecom.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("WeCom configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("WeCom configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("WeCom configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("WeCom configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
