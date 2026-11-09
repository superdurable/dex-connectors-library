# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, FIND_CONTACT_DEFINITION
from ..models import Contact, FindContactInput

class Connection:

    __slots__ = ('_find_contact', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        find_contact: Query[FindContactInput, Contact],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or find_contact is None:
            raise ValueError("Microsoft Dynamics 365 Sales connection provider and operations are required")
        if find_contact.definition().operation != FIND_CONTACT_DEFINITION.operation:
            raise ValueError("Microsoft Dynamics 365 Sales connection operation identity differs")
        self.reference = reference
        self._find_contact = find_contact
        self._client = client

    def find_contact(self) -> Query[FindContactInput, Contact]:
        return self._find_contact

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "microsoft_dynamics_365_sales.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Microsoft Dynamics 365 Sales connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.microsoft_dynamics_365_sales.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Microsoft Dynamics 365 Sales client is required")
    return Connection(reference, client.find_contact(), client)

setattr(new_connection, '__module__', 'dex_connectors.microsoft_dynamics_365_sales.connection')


def new_connection_with_operations(
    find_contact: Query[FindContactInput, Contact],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, find_contact)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.microsoft_dynamics_365_sales.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Microsoft Dynamics 365 Sales stored credential does not match the connector credential fields")
        if set(material) - set(('client_secret',)):
            raise ValueError("Microsoft Dynamics 365 Sales stored credential does not match the connector credential fields")
        client_secret = material.get("client_secret")
        if client_secret is not None and not (isinstance(client_secret, str)):
            raise ValueError("Microsoft Dynamics 365 Sales credential client_secret has an invalid type")
        if not client_secret:
            raise ValueError("Microsoft Dynamics 365 Sales credential client_secret is required")
        return Credentials(SecretString(client_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Microsoft Dynamics 365 Sales connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Microsoft Dynamics 365 Sales project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Microsoft Dynamics 365 Sales project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.microsoft_dynamics_365_sales.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"environmentUrl", "tenantId", "clientId", "timeout", "maxResponseBytes"}:
        raise ValueError("Microsoft Dynamics 365 Sales configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "environmentUrl" in value:
        environment_url = value["environmentUrl"]
        if not (isinstance(environment_url, str)):
            raise ValueError("Microsoft Dynamics 365 Sales configuration environmentUrl has an invalid type")
        config_arguments['environment_url'] = environment_url
    else:
        raise ValueError("Microsoft Dynamics 365 Sales configuration environmentUrl is required")
    if "tenantId" in value:
        tenant_id = value["tenantId"]
        if not (isinstance(tenant_id, str)):
            raise ValueError("Microsoft Dynamics 365 Sales configuration tenantId has an invalid type")
        config_arguments['tenant_id'] = tenant_id
    else:
        raise ValueError("Microsoft Dynamics 365 Sales configuration tenantId is required")
    if "clientId" in value:
        client_id = value["clientId"]
        if not (isinstance(client_id, str)):
            raise ValueError("Microsoft Dynamics 365 Sales configuration clientId has an invalid type")
        config_arguments['client_id'] = client_id
    else:
        raise ValueError("Microsoft Dynamics 365 Sales configuration clientId is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Microsoft Dynamics 365 Sales configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Microsoft Dynamics 365 Sales configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
