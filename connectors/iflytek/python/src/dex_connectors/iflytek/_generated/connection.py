# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_CHAT_COMPLETION_DEFINITION
from ..models import ChatCompletion, CreateChatCompletionInput

class Connection:

    __slots__ = ('_create_chat_completion', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        create_chat_completion: Query[CreateChatCompletionInput, ChatCompletion],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or create_chat_completion is None:
            raise ValueError("iFLYTEK Spark connection provider and operations are required")
        if create_chat_completion.definition().operation != CREATE_CHAT_COMPLETION_DEFINITION.operation:
            raise ValueError("iFLYTEK Spark connection operation identity differs")
        self.reference = reference
        self._create_chat_completion = create_chat_completion
        self._client = client

    def create_chat_completion(self) -> Query[CreateChatCompletionInput, ChatCompletion]:
        return self._create_chat_completion

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "iflytek.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("iFLYTEK Spark connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.iflytek.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("iFLYTEK Spark client is required")
    return Connection(reference, client.create_chat_completion(), client)

setattr(new_connection, '__module__', 'dex_connectors.iflytek.connection')


def new_connection_with_operations(
    create_chat_completion: Query[CreateChatCompletionInput, ChatCompletion],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, create_chat_completion)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.iflytek.connection')


class CredentialMaterialError(ValueError):
    "Stored iFLYTEK Spark credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("iFLYTEK Spark stored credential does not match the connector credential fields")
        if set(material) - set(('apiPassword',)):
            raise CredentialMaterialError("iFLYTEK Spark stored credential does not match the connector credential fields")
        api_password = material.get("apiPassword")
        if api_password is not None and not (isinstance(api_password, str)):
            raise CredentialMaterialError("iFLYTEK Spark credential apiPassword has an invalid type")
        if not api_password:
            raise CredentialMaterialError("iFLYTEK Spark credential apiPassword is required")
        return Credentials(SecretString(api_password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("iFLYTEK Spark connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("iFLYTEK Spark project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("iFLYTEK Spark project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.iflytek.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes", "model", "maxOutputTokens"}:
        raise ValueError("iFLYTEK Spark configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("iFLYTEK Spark configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("iFLYTEK Spark configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("iFLYTEK Spark configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "model" in value:
        model = value["model"]
        if not (isinstance(model, str)):
            raise ValueError("iFLYTEK Spark configuration model has an invalid type")
        config_arguments['model'] = model
    if "maxOutputTokens" in value:
        max_output_tokens = value["maxOutputTokens"]
        if not (type(max_output_tokens) is int):
            raise ValueError("iFLYTEK Spark configuration maxOutputTokens has an invalid type")
        config_arguments['max_output_tokens'] = max_output_tokens
    return Config(**config_arguments)
