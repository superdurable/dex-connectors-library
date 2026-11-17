# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, QUERY_ORDER_STATUS_DEFINITION
from ..models import OrderStatus, QueryOrderStatusInput

class Connection:

    __slots__ = ('_query_order_status', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_order_status: Query[QueryOrderStatusInput, OrderStatus],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_order_status is None:
            raise ValueError("Dada Express connection provider and operations are required")
        if query_order_status.definition().operation != QUERY_ORDER_STATUS_DEFINITION.operation:
            raise ValueError("Dada Express connection operation identity differs")
        self.reference = reference
        self._query_order_status = query_order_status
        self._client = client

    def query_order_status(self) -> Query[QueryOrderStatusInput, OrderStatus]:
        return self._query_order_status

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "dada.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Dada Express connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.dada.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Dada Express client is required")
    return Connection(reference, client.query_order_status(), client)

setattr(new_connection, '__module__', 'dex_connectors.dada.connection')


def new_connection_with_operations(
    query_order_status: Query[QueryOrderStatusInput, OrderStatus],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_order_status)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.dada.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Dada Express stored credential does not match the connector credential fields")
        if set(material) - set(('appKey', 'appSecret', 'sourceID')):
            raise ValueError("Dada Express stored credential does not match the connector credential fields")
        app_key = material.get("appKey")
        if app_key is not None and not (isinstance(app_key, str)):
            raise ValueError("Dada Express credential appKey has an invalid type")
        app_secret = material.get("appSecret")
        if app_secret is not None and not (isinstance(app_secret, str)):
            raise ValueError("Dada Express credential appSecret has an invalid type")
        source_id = material.get("sourceID")
        if source_id is not None and not (isinstance(source_id, str)):
            raise ValueError("Dada Express credential sourceID has an invalid type")
        if not app_key:
            raise ValueError("Dada Express credential appKey is required")
        if not app_secret:
            raise ValueError("Dada Express credential appSecret is required")
        if not source_id:
            raise ValueError("Dada Express credential sourceID is required")
        return Credentials(app_key, SecretString(app_secret), source_id)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Dada Express connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Dada Express project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Dada Express project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.dada.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Dada Express configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Dada Express configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Dada Express configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Dada Express configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
