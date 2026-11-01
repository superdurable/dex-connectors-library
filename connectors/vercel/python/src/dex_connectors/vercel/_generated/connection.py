# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation bindings for one named Dex connection."""

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_DEPLOYMENT_DEFINITION, LIST_DEPLOYMENTS_DEFINITION
from ..models import Deployment, DeploymentsPage, GetDeploymentInput, ListDeploymentsInput

class Connection:
    """Nonserializable Vercel operation binding."""

    __slots__ = ('_list_deployments', '_get_deployment', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_deployments: Query[ListDeploymentsInput, DeploymentsPage],
        get_deployment: Query[GetDeploymentInput, Deployment],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_deployments is None or get_deployment is None:
            raise ValueError("Vercel connection provider and operations are required")
        if list_deployments.definition().operation != LIST_DEPLOYMENTS_DEFINITION.operation:
            raise ValueError("Vercel connection operation identity differs")
        if get_deployment.definition().operation != GET_DEPLOYMENT_DEFINITION.operation:
            raise ValueError("Vercel connection operation identity differs")
        self.reference = reference
        self._list_deployments = list_deployments
        self._get_deployment = get_deployment
        self._client = client

    def list_deployments(self) -> Query[ListDeploymentsInput, DeploymentsPage]:
        """Return the list_deployments operation for a Dex Step."""
        return self._list_deployments

    def get_deployment(self) -> Query[GetDeploymentInput, Deployment]:
        """Return the get_deployment operation for a Dex Step."""
        return self._get_deployment

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "vercel.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Vercel connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.vercel.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a provider client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Vercel client is required")
    return Connection(reference, client.list_deployments(), client.get_deployment(), client)

setattr(new_connection, '__module__', 'dex_connectors.vercel.connection')


def new_connection_with_operations(
    list_deployments: Query[ListDeploymentsInput, DeploymentsPage],
    get_deployment: Query[GetDeploymentInput, Deployment],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same logical connection identity."""
    return Connection(reference, list_deployments, get_deployment)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.vercel.connection')


class _ProjectCredentialSource:
    """Resolve current secret material at each provider attempt."""

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        access_token = material.get("access_token")
        if not isinstance(access_token, str) or not access_token:
            raise ValueError("Vercel credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared project settings and resolve provider credentials per call."""
    if project is None:
        raise ValueError("Vercel connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Vercel project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Vercel project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.vercel.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Vercel configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Vercel configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Vercel configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Vercel configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
