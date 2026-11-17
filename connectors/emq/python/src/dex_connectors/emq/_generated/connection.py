# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_DEPLOYMENTS_DEFINITION
from ..models import ListDeploymentsInput, ListDeploymentsOutput

class Connection:

    __slots__ = ('_list_deployments', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_deployments: Query[ListDeploymentsInput, ListDeploymentsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_deployments is None:
            raise ValueError("EMQX Cloud connection provider and operations are required")
        if list_deployments.definition().operation != LIST_DEPLOYMENTS_DEFINITION.operation:
            raise ValueError("EMQX Cloud connection operation identity differs")
        self.reference = reference
        self._list_deployments = list_deployments
        self._client = client

    def list_deployments(self) -> Query[ListDeploymentsInput, ListDeploymentsOutput]:
        return self._list_deployments

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "emq.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("EMQX Cloud connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.emq.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("EMQX Cloud client is required")
    return Connection(reference, client.list_deployments(), client)

setattr(new_connection, '__module__', 'dex_connectors.emq.connection')


def new_connection_with_operations(
    list_deployments: Query[ListDeploymentsInput, ListDeploymentsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_deployments)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.emq.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("EMQX Cloud stored credential does not match the connector credential fields")
        if set(material) - set(('platform_api_key', 'platform_api_secret')):
            raise ValueError("EMQX Cloud stored credential does not match the connector credential fields")
        platform_api_key = material.get("platform_api_key")
        if platform_api_key is not None and not (isinstance(platform_api_key, str)):
            raise ValueError("EMQX Cloud credential platform_api_key has an invalid type")
        platform_api_secret = material.get("platform_api_secret")
        if platform_api_secret is not None and not (isinstance(platform_api_secret, str)):
            raise ValueError("EMQX Cloud credential platform_api_secret has an invalid type")
        if not platform_api_key:
            raise ValueError("EMQX Cloud credential platform_api_key is required")
        if not platform_api_secret:
            raise ValueError("EMQX Cloud credential platform_api_secret is required")
        return Credentials(SecretString(platform_api_key), SecretString(platform_api_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("EMQX Cloud connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("EMQX Cloud project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("EMQX Cloud project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.emq.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"platformEndpoint", "timeout", "maxResponseBytes"}:
        raise ValueError("EMQX Cloud configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "platformEndpoint" in value:
        platform_endpoint = value["platformEndpoint"]
        if not (isinstance(platform_endpoint, str)):
            raise ValueError("EMQX Cloud configuration platformEndpoint has an invalid type")
        config_arguments['platform_endpoint'] = platform_endpoint
    else:
        raise ValueError("EMQX Cloud configuration platformEndpoint is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("EMQX Cloud configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("EMQX Cloud configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
