# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GENERATE_TEXT_DEFINITION
from ..models import GenerateTextInput, GenerateTextOutput

class Connection:

    __slots__ = ('_generate_text', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        generate_text: Query[GenerateTextInput, GenerateTextOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or generate_text is None:
            raise ValueError("Zhipu AI connection provider and operations are required")
        if generate_text.definition().operation != GENERATE_TEXT_DEFINITION.operation:
            raise ValueError("Zhipu AI connection operation identity differs")
        self.reference = reference
        self._generate_text = generate_text
        self._client = client

    def generate_text(self) -> Query[GenerateTextInput, GenerateTextOutput]:
        return self._generate_text

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "zhipu.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Zhipu AI connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.zhipu.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Zhipu AI client is required")
    return Connection(reference, client.generate_text(), client)

setattr(new_connection, '__module__', 'dex_connectors.zhipu.connection')


def new_connection_with_operations(
    generate_text: Query[GenerateTextInput, GenerateTextOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, generate_text)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.zhipu.connection')


class CredentialMaterialError(ValueError):
    "Stored Zhipu AI credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Zhipu AI stored credential does not match the connector credential fields")
        if set(material) - set(('api_key',)):
            raise CredentialMaterialError("Zhipu AI stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise CredentialMaterialError("Zhipu AI credential api_key has an invalid type")
        if not api_key:
            raise CredentialMaterialError("Zhipu AI credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Zhipu AI connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Zhipu AI project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Zhipu AI project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.zhipu.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"model", "timeout", "maxResponseBytes"}:
        raise ValueError("Zhipu AI configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "model" in value:
        model = value["model"]
        if not (isinstance(model, str)):
            raise ValueError("Zhipu AI configuration model has an invalid type")
        config_arguments['model'] = model
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Zhipu AI configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Zhipu AI configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
