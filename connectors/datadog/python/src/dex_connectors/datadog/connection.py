# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Datadog connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    LIST_MONITORS_DEFINITION,
    QUERY_METRICS_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import ListMonitorsInput, MetricsQuery, MonitorsPage, QueryMetricsInput


class Connection:
    """Nonserializable Datadog operation bindings for a named project connection."""

    __slots__ = ("_query_metrics", "_list_monitors", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        query_metrics: Query[QueryMetricsInput, MetricsQuery],
        list_monitors: Query[ListMonitorsInput, MonitorsPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_metrics is None or list_monitors is None:
            raise ValueError("Datadog connection provider and operations are required")
        if (
            query_metrics.definition().operation != QUERY_METRICS_DEFINITION.operation
            or list_monitors.definition().operation != LIST_MONITORS_DEFINITION.operation
        ):
            raise ValueError("Datadog connection operation identity differs")
        self.reference = reference
        self._query_metrics = query_metrics
        self._list_monitors = list_monitors
        self._client = client

    def query_metrics(self) -> Query[QueryMetricsInput, MetricsQuery]:
        """Return the metric query Query used by an application Step."""
        return self._query_metrics

    def list_monitors(self) -> Query[ListMonitorsInput, MonitorsPage]:
        """Return the monitor page Query used by an application Step."""
        return self._list_monitors

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "datadog.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Datadog connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Datadog client is required")
    return Connection(reference, client.query_metrics(), client.list_monitors(), client)


def new_connection_with_operations(
    query_metrics: Query[QueryMetricsInput, MetricsQuery],
    list_monitors: Query[ListMonitorsInput, MonitorsPage],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, query_metrics, list_monitors)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        application_key = material.get("application_key")
        if (
            not isinstance(api_key, str)
            or not api_key
            or not isinstance(application_key, str)
            or not application_key
        ):
            raise ValueError("Datadog credentials api_key and application_key are required")
        return Credentials(SecretString(api_key), SecretString(application_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current API and application keys per call."""
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


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Datadog configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Datadog configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Datadog configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
