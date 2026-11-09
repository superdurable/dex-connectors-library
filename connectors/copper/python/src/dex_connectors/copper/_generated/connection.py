# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_LEAD_DEFINITION, LIST_LEAD_STATUSES_DEFINITION
from ..models import GetLeadInput, LeadMetadata, LeadStatusesList, ListLeadStatusesInput

class Connection:

    __slots__ = ('_list_lead_statuses', '_get_lead', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_lead_statuses: Query[ListLeadStatusesInput, LeadStatusesList],
        get_lead: Query[GetLeadInput, LeadMetadata],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_lead_statuses is None or get_lead is None:
            raise ValueError("Copper CRM connection provider and operations are required")
        if list_lead_statuses.definition().operation != LIST_LEAD_STATUSES_DEFINITION.operation:
            raise ValueError("Copper CRM connection operation identity differs")
        if get_lead.definition().operation != GET_LEAD_DEFINITION.operation:
            raise ValueError("Copper CRM connection operation identity differs")
        self.reference = reference
        self._list_lead_statuses = list_lead_statuses
        self._get_lead = get_lead
        self._client = client

    def list_lead_statuses(self) -> Query[ListLeadStatusesInput, LeadStatusesList]:
        return self._list_lead_statuses

    def get_lead(self) -> Query[GetLeadInput, LeadMetadata]:
        return self._get_lead

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "copper.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Copper CRM connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.copper.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Copper CRM client is required")
    return Connection(reference, client.list_lead_statuses(), client.get_lead(), client)

setattr(new_connection, '__module__', 'dex_connectors.copper.connection')


def new_connection_with_operations(
    list_lead_statuses: Query[ListLeadStatusesInput, LeadStatusesList],
    get_lead: Query[GetLeadInput, LeadMetadata],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_lead_statuses, get_lead)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.copper.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Copper CRM stored credential does not match the connector credential fields")
        if set(material) - set(('api_key', 'user_email')):
            raise ValueError("Copper CRM stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Copper CRM credential api_key has an invalid type")
        user_email = material.get("user_email")
        if user_email is not None and not (isinstance(user_email, str)):
            raise ValueError("Copper CRM credential user_email has an invalid type")
        if not api_key:
            raise ValueError("Copper CRM credential api_key is required")
        if not user_email:
            raise ValueError("Copper CRM credential user_email is required")
        return Credentials(SecretString(api_key), SecretString(user_email))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Copper CRM connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Copper CRM project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Copper CRM project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.copper.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Copper CRM configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Copper CRM configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Copper CRM configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Copper CRM configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
