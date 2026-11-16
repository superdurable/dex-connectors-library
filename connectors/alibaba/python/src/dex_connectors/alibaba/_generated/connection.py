# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, DESCRIBE_REGIONS_DEFINITION
from ..models import DescribeRegionsInput, DescribeRegionsOutput

class Connection:

    __slots__ = ('_describe_regions', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        describe_regions: Query[DescribeRegionsInput, DescribeRegionsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or describe_regions is None:
            raise ValueError("Alibaba Cloud connection provider and operations are required")
        if describe_regions.definition().operation != DESCRIBE_REGIONS_DEFINITION.operation:
            raise ValueError("Alibaba Cloud connection operation identity differs")
        self.reference = reference
        self._describe_regions = describe_regions
        self._client = client

    def describe_regions(self) -> Query[DescribeRegionsInput, DescribeRegionsOutput]:
        return self._describe_regions

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "alibaba.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Alibaba Cloud connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.alibaba.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Alibaba Cloud client is required")
    return Connection(reference, client.describe_regions(), client)

setattr(new_connection, '__module__', 'dex_connectors.alibaba.connection')


def new_connection_with_operations(
    describe_regions: Query[DescribeRegionsInput, DescribeRegionsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, describe_regions)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.alibaba.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Alibaba Cloud stored credential does not match the connector credential fields")
        if set(material) - set(('access_key_id', 'access_key_secret')):
            raise ValueError("Alibaba Cloud stored credential does not match the connector credential fields")
        access_key_id = material.get("access_key_id")
        if access_key_id is not None and not (isinstance(access_key_id, str)):
            raise ValueError("Alibaba Cloud credential access_key_id has an invalid type")
        access_key_secret = material.get("access_key_secret")
        if access_key_secret is not None and not (isinstance(access_key_secret, str)):
            raise ValueError("Alibaba Cloud credential access_key_secret has an invalid type")
        if not access_key_id:
            raise ValueError("Alibaba Cloud credential access_key_id is required")
        if not access_key_secret:
            raise ValueError("Alibaba Cloud credential access_key_secret is required")
        return Credentials(SecretString(access_key_id), SecretString(access_key_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Alibaba Cloud connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Alibaba Cloud project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Alibaba Cloud project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.alibaba.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Alibaba Cloud configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Alibaba Cloud configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Alibaba Cloud configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Alibaba Cloud configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
