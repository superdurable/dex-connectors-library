# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation
from dex_connectors.sdk.projectconfig import LoadedProject

from ..client import Client, Config
from .definitions import CONNECTOR_ID, SET_USER_PROPERTIES_DEFINITION
from ..models import SetUserPropertiesInput, UserPropertiesResult

class Connection:

    __slots__ = ('_set_user_properties', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        set_user_properties: Mutation[SetUserPropertiesInput, UserPropertiesResult],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or set_user_properties is None:
            raise ValueError("Heap connection provider and operations are required")
        if set_user_properties.definition().operation != SET_USER_PROPERTIES_DEFINITION.operation:
            raise ValueError("Heap connection operation identity differs")
        self.reference = reference
        self._set_user_properties = set_user_properties
        self._client = client

    def set_user_properties(self) -> Mutation[SetUserPropertiesInput, UserPropertiesResult]:
        return self._set_user_properties

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "heap.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Heap connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.heap.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Heap client is required")
    return Connection(reference, client.set_user_properties(), client)

setattr(new_connection, '__module__', 'dex_connectors.heap.connection')


def new_connection_with_operations(
    set_user_properties: Mutation[SetUserPropertiesInput, UserPropertiesResult],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, set_user_properties)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.heap.connection')


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Heap connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Heap project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Heap project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.heap.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "appId", "timeout", "maxResponseBytes"}:
        raise ValueError("Heap configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Heap configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "appId" in value:
        app_id = value["appId"]
        if not (isinstance(app_id, str)):
            raise ValueError("Heap configuration appId has an invalid type")
        config_arguments['app_id'] = app_id
    else:
        raise ValueError("Heap configuration appId is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Heap configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Heap configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
