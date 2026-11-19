# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_SERVICE_USAGE_DEFINITION
from ..models import GetServiceUsageInput, ServiceUsage

class Connection:

    __slots__ = ('_get_service_usage', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_service_usage: Query[GetServiceUsageInput, ServiceUsage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_service_usage is None:
            raise ValueError("UPYUN connection provider and operations are required")
        if get_service_usage.definition().operation != GET_SERVICE_USAGE_DEFINITION.operation:
            raise ValueError("UPYUN connection operation identity differs")
        self.reference = reference
        self._get_service_usage = get_service_usage
        self._client = client

    def get_service_usage(self) -> Query[GetServiceUsageInput, ServiceUsage]:
        return self._get_service_usage

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "upyun.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("UPYUN connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.upyun.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("UPYUN client is required")
    return Connection(reference, client.get_service_usage(), client)

setattr(new_connection, '__module__', 'dex_connectors.upyun.connection')


def new_connection_with_operations(
    get_service_usage: Query[GetServiceUsageInput, ServiceUsage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_service_usage)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.upyun.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("UPYUN stored credential does not match the connector credential fields")
        if set(material) - set(('operator', 'password')):
            raise ValueError("UPYUN stored credential does not match the connector credential fields")
        operator = material.get("operator")
        if operator is not None and not (isinstance(operator, str)):
            raise ValueError("UPYUN credential operator has an invalid type")
        password = material.get("password")
        if password is not None and not (isinstance(password, str)):
            raise ValueError("UPYUN credential password has an invalid type")
        if not operator:
            raise ValueError("UPYUN credential operator is required")
        if not password:
            raise ValueError("UPYUN credential password is required")
        return Credentials(operator, SecretString(password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("UPYUN connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("UPYUN project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("UPYUN project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.upyun.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "bucket", "timeout", "maxResponseBytes"}:
        raise ValueError("UPYUN configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("UPYUN configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "bucket" in value:
        bucket = value["bucket"]
        if not (isinstance(bucket, str)):
            raise ValueError("UPYUN configuration bucket has an invalid type")
        config_arguments['bucket'] = bucket
    else:
        raise ValueError("UPYUN configuration bucket is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("UPYUN configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("UPYUN configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
