# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_PARSE_STATUS_DEFINITION
from ..models import GetParseStatusInput, GetParseStatusOutput

class Connection:

    __slots__ = ('_get_parse_status', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_parse_status: Query[GetParseStatusInput, GetParseStatusOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_parse_status is None:
            raise ValueError("Doc2X Parse Status connection provider and operations are required")
        if get_parse_status.definition().operation != GET_PARSE_STATUS_DEFINITION.operation:
            raise ValueError("Doc2X Parse Status connection operation identity differs")
        self.reference = reference
        self._get_parse_status = get_parse_status
        self._client = client

    def get_parse_status(self) -> Query[GetParseStatusInput, GetParseStatusOutput]:
        return self._get_parse_status

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "doc2x.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Doc2X Parse Status connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.doc2x.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Doc2X Parse Status client is required")
    return Connection(reference, client.get_parse_status(), client)

setattr(new_connection, '__module__', 'dex_connectors.doc2x.connection')


def new_connection_with_operations(
    get_parse_status: Query[GetParseStatusInput, GetParseStatusOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_parse_status)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.doc2x.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Doc2X Parse Status stored credential does not match the connector credential fields")
        if set(material) - set(('apiKey',)):
            raise ValueError("Doc2X Parse Status stored credential does not match the connector credential fields")
        api_key = material.get("apiKey")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Doc2X Parse Status credential apiKey has an invalid type")
        if not api_key:
            raise ValueError("Doc2X Parse Status credential apiKey is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Doc2X Parse Status connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Doc2X Parse Status project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Doc2X Parse Status project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.doc2x.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("Doc2X Parse Status configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Doc2X Parse Status configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Doc2X Parse Status configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
