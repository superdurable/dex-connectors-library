# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, QUERY_WAYBILL_TRACE_DEFINITION
from ..models import QueryWaybillTraceInput, WaybillTrace

class Connection:

    __slots__ = ('_query_waybill_trace', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_waybill_trace: Query[QueryWaybillTraceInput, WaybillTrace],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_waybill_trace is None:
            raise ValueError("Cainiao Waybill connection provider and operations are required")
        if query_waybill_trace.definition().operation != QUERY_WAYBILL_TRACE_DEFINITION.operation:
            raise ValueError("Cainiao Waybill connection operation identity differs")
        self.reference = reference
        self._query_waybill_trace = query_waybill_trace
        self._client = client

    def query_waybill_trace(self) -> Query[QueryWaybillTraceInput, WaybillTrace]:
        return self._query_waybill_trace

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "cainiao.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Cainiao Waybill connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.cainiao.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Cainiao Waybill client is required")
    return Connection(reference, client.query_waybill_trace(), client)

setattr(new_connection, '__module__', 'dex_connectors.cainiao.connection')


def new_connection_with_operations(
    query_waybill_trace: Query[QueryWaybillTraceInput, WaybillTrace],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_waybill_trace)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.cainiao.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Cainiao Waybill stored credential does not match the connector credential fields")
        if set(material) - set(('appKey', 'appSecret', 'sessionKey')):
            raise ValueError("Cainiao Waybill stored credential does not match the connector credential fields")
        app_key = material.get("appKey")
        if app_key is not None and not (isinstance(app_key, str)):
            raise ValueError("Cainiao Waybill credential appKey has an invalid type")
        app_secret = material.get("appSecret")
        if app_secret is not None and not (isinstance(app_secret, str)):
            raise ValueError("Cainiao Waybill credential appSecret has an invalid type")
        session_key = material.get("sessionKey")
        if session_key is not None and not (isinstance(session_key, str)):
            raise ValueError("Cainiao Waybill credential sessionKey has an invalid type")
        if not app_key:
            raise ValueError("Cainiao Waybill credential appKey is required")
        if not app_secret:
            raise ValueError("Cainiao Waybill credential appSecret is required")
        if not session_key:
            raise ValueError("Cainiao Waybill credential sessionKey is required")
        return Credentials(app_key, SecretString(app_secret), SecretString(session_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Cainiao Waybill connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Cainiao Waybill project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Cainiao Waybill project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.cainiao.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Cainiao Waybill configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Cainiao Waybill configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Cainiao Waybill configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Cainiao Waybill configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
