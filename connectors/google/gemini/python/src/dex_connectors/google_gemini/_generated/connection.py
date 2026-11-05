# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GENERATE_CONTENT_DEFINITION
from ..models import GenerateContentRequest, GenerateContentResponse

class Connection:

    __slots__ = ('_generate_content', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        generate_content: Query[GenerateContentRequest, GenerateContentResponse],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or generate_content is None:
            raise ValueError("Gemini connection provider and operations are required")
        if generate_content.definition().operation != GENERATE_CONTENT_DEFINITION.operation:
            raise ValueError("Gemini connection operation identity differs")
        self.reference = reference
        self._generate_content = generate_content
        self._client = client

    def generate_content(self) -> Query[GenerateContentRequest, GenerateContentResponse]:
        return self._generate_content

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "google_gemini.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Gemini connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.google_gemini.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Gemini client is required")
    return Connection(reference, client.generate_content(), client)

setattr(new_connection, '__module__', 'dex_connectors.google_gemini.connection')


def new_connection_with_operations(
    generate_content: Query[GenerateContentRequest, GenerateContentResponse],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, generate_content)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.google_gemini.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Gemini credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Gemini connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Gemini project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Gemini project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.google_gemini.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"model", "endpoint", "maxResponseBytes"}:
        raise ValueError("Gemini configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "model" in value:
        model = value["model"]
        if not (isinstance(model, str)):
            raise ValueError("Gemini configuration model has an invalid type")
        config_arguments['model'] = model
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Gemini configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Gemini configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
