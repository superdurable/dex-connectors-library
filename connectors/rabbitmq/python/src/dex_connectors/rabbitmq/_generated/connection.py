# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_QUEUE_METRICS_DEFINITION
from ..models import GetQueueMetricsInput, QueueMetrics

class Connection:

    __slots__ = ('_get_queue_metrics', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_queue_metrics: Query[GetQueueMetricsInput, QueueMetrics],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_queue_metrics is None:
            raise ValueError("RabbitMQ connection provider and operations are required")
        if get_queue_metrics.definition().operation != GET_QUEUE_METRICS_DEFINITION.operation:
            raise ValueError("RabbitMQ connection operation identity differs")
        self.reference = reference
        self._get_queue_metrics = get_queue_metrics
        self._client = client

    def get_queue_metrics(self) -> Query[GetQueueMetricsInput, QueueMetrics]:
        return self._get_queue_metrics

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "rabbitmq.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("RabbitMQ connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.rabbitmq.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("RabbitMQ client is required")
    return Connection(reference, client.get_queue_metrics(), client)

setattr(new_connection, '__module__', 'dex_connectors.rabbitmq.connection')


def new_connection_with_operations(
    get_queue_metrics: Query[GetQueueMetricsInput, QueueMetrics],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_queue_metrics)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.rabbitmq.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("RabbitMQ stored credential does not match the connector credential fields")
        if set(material) - set(('username', 'password')):
            raise ValueError("RabbitMQ stored credential does not match the connector credential fields")
        username = material.get("username")
        if username is not None and not (isinstance(username, str)):
            raise ValueError("RabbitMQ credential username has an invalid type")
        password = material.get("password")
        if password is not None and not (isinstance(password, str)):
            raise ValueError("RabbitMQ credential password has an invalid type")
        if not username:
            raise ValueError("RabbitMQ credential username is required")
        if not password:
            raise ValueError("RabbitMQ credential password is required")
        return Credentials(username, SecretString(password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("RabbitMQ connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("RabbitMQ project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("RabbitMQ project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.rabbitmq.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"managementUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("RabbitMQ configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "managementUrl" in value:
        management_url = value["managementUrl"]
        if not (isinstance(management_url, str)):
            raise ValueError("RabbitMQ configuration managementUrl has an invalid type")
        config_arguments['management_url'] = management_url
    else:
        raise ValueError("RabbitMQ configuration managementUrl is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("RabbitMQ configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("RabbitMQ configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
