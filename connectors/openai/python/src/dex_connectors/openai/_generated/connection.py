# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_RESPONSE_DEFINITION, RETRIEVE_RESPONSE_DEFINITION
from ..models import CreateRequest, Response, RetrieveRequest

class Connection:

    __slots__ = ('_create_response', '_retrieve_response', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        create_response: Mutation[CreateRequest, Response],
        retrieve_response: Query[RetrieveRequest, Response],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or create_response is None or retrieve_response is None:
            raise ValueError("OpenAI connection provider and operations are required")
        if create_response.definition().operation != CREATE_RESPONSE_DEFINITION.operation:
            raise ValueError("OpenAI connection operation identity differs")
        if retrieve_response.definition().operation != RETRIEVE_RESPONSE_DEFINITION.operation:
            raise ValueError("OpenAI connection operation identity differs")
        self.reference = reference
        self._create_response = create_response
        self._retrieve_response = retrieve_response
        self._client = client

    def create_response(self) -> Mutation[CreateRequest, Response]:
        return self._create_response

    def retrieve_response(self) -> Query[RetrieveRequest, Response]:
        return self._retrieve_response

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "openai.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("OpenAI connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.openai.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("OpenAI client is required")
    return Connection(reference, client.create_response(), client.retrieve_response(), client)

setattr(new_connection, '__module__', 'dex_connectors.openai.connection')


def new_connection_with_operations(
    create_response: Mutation[CreateRequest, Response],
    retrieve_response: Query[RetrieveRequest, Response],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, create_response, retrieve_response)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.openai.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("OpenAI stored credential does not match the connector credential fields")
        if set(material) - set(('api_key',)):
            raise ValueError("OpenAI stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("OpenAI credential api_key has an invalid type")
        if not api_key:
            raise ValueError("OpenAI credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("OpenAI connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("OpenAI project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("OpenAI project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.openai.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"model", "endpoint", "maxResponseBytes", "maxSseEventBytes"}:
        raise ValueError("OpenAI configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "model" in value:
        model = value["model"]
        if not (isinstance(model, str)):
            raise ValueError("OpenAI configuration model has an invalid type")
        config_arguments['model'] = model
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("OpenAI configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("OpenAI configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxSseEventBytes" in value:
        max_sse_event_bytes = value["maxSseEventBytes"]
        if not (type(max_sse_event_bytes) is int):
            raise ValueError("OpenAI configuration maxSseEventBytes has an invalid type")
        config_arguments['max_sse_event_bytes'] = max_sse_event_bytes
    return Config(**config_arguments)
