# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_MONITORS_DEFINITION, QUERY_METRICS_DEFINITION
from ..models import ListMonitorsInput, MetricsQuery, MonitorsPage, QueryMetricsInput

class Connection:

    __slots__ = ('_query_metrics', '_list_monitors', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_metrics: Query[QueryMetricsInput, MetricsQuery],
        list_monitors: Query[ListMonitorsInput, MonitorsPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_metrics is None or list_monitors is None:
            raise ValueError("Datadog connection provider and operations are required")
        if query_metrics.definition().operation != QUERY_METRICS_DEFINITION.operation:
            raise ValueError("Datadog connection operation identity differs")
        if list_monitors.definition().operation != LIST_MONITORS_DEFINITION.operation:
            raise ValueError("Datadog connection operation identity differs")
        self.reference = reference
        self._query_metrics = query_metrics
        self._list_monitors = list_monitors
        self._client = client

    def query_metrics(self) -> Query[QueryMetricsInput, MetricsQuery]:
        return self._query_metrics

    def list_monitors(self) -> Query[ListMonitorsInput, MonitorsPage]:
        return self._list_monitors

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "datadog.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Datadog connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.datadog.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Datadog client is required")
    return Connection(reference, client.query_metrics(), client.list_monitors(), client)

setattr(new_connection, '__module__', 'dex_connectors.datadog.connection')


def new_connection_with_operations(
    query_metrics: Query[QueryMetricsInput, MetricsQuery],
    list_monitors: Query[ListMonitorsInput, MonitorsPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_metrics, list_monitors)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.datadog.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Datadog stored credential does not match the connector credential fields")
        if set(material) - set(('api_key', 'application_key')):
            raise ValueError("Datadog stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Datadog credential api_key has an invalid type")
        application_key = material.get("application_key")
        if application_key is not None and not (isinstance(application_key, str)):
            raise ValueError("Datadog credential application_key has an invalid type")
        if not api_key:
            raise ValueError("Datadog credential api_key is required")
        if not application_key:
            raise ValueError("Datadog credential application_key is required")
        return Credentials(SecretString(api_key), SecretString(application_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Datadog connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Datadog project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Datadog project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.datadog.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Datadog configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Datadog configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Datadog configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Datadog configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
