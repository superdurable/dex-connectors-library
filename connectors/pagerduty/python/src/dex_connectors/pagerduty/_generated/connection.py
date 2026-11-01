# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_INCIDENT_DEFINITION, LIST_INCIDENTS_DEFINITION
from ..models import GetIncidentInput, Incident, IncidentPage, ListIncidentsInput

class Connection:

    __slots__ = ('_list_incidents', '_get_incident', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_incidents: Query[ListIncidentsInput, IncidentPage],
        get_incident: Query[GetIncidentInput, Incident],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_incidents is None or get_incident is None:
            raise ValueError("PagerDuty connection provider and operations are required")
        if list_incidents.definition().operation != LIST_INCIDENTS_DEFINITION.operation:
            raise ValueError("PagerDuty connection operation identity differs")
        if get_incident.definition().operation != GET_INCIDENT_DEFINITION.operation:
            raise ValueError("PagerDuty connection operation identity differs")
        self.reference = reference
        self._list_incidents = list_incidents
        self._get_incident = get_incident
        self._client = client

    def list_incidents(self) -> Query[ListIncidentsInput, IncidentPage]:
        return self._list_incidents

    def get_incident(self) -> Query[GetIncidentInput, Incident]:
        return self._get_incident

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "pagerduty.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("PagerDuty connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.pagerduty.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("PagerDuty client is required")
    return Connection(reference, client.list_incidents(), client.get_incident(), client)

setattr(new_connection, '__module__', 'dex_connectors.pagerduty.connection')


def new_connection_with_operations(
    list_incidents: Query[ListIncidentsInput, IncidentPage],
    get_incident: Query[GetIncidentInput, Incident],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_incidents, get_incident)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.pagerduty.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("PagerDuty credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("PagerDuty connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("PagerDuty project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("PagerDuty project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.pagerduty.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("PagerDuty configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("PagerDuty configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("PagerDuty configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("PagerDuty configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
