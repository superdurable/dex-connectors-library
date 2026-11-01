# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, EXPORT_EVENTS_DEFINITION, TRACK_EVENT_DEFINITION
from ..models import EventImportResult, ExportEventsInput, ExportedEvents, TrackEventInput

class Connection:

    __slots__ = ('_track_event', '_export_events', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        track_event: Mutation[TrackEventInput, EventImportResult],
        export_events: Query[ExportEventsInput, ExportedEvents],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or track_event is None or export_events is None:
            raise ValueError("Mixpanel connection provider and operations are required")
        if track_event.definition().operation != TRACK_EVENT_DEFINITION.operation:
            raise ValueError("Mixpanel connection operation identity differs")
        if export_events.definition().operation != EXPORT_EVENTS_DEFINITION.operation:
            raise ValueError("Mixpanel connection operation identity differs")
        self.reference = reference
        self._track_event = track_event
        self._export_events = export_events
        self._client = client

    def track_event(self) -> Mutation[TrackEventInput, EventImportResult]:
        return self._track_event

    def export_events(self) -> Query[ExportEventsInput, ExportedEvents]:
        return self._export_events

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "mixpanel.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Mixpanel connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.mixpanel.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Mixpanel client is required")
    return Connection(reference, client.track_event(), client.export_events(), client)

setattr(new_connection, '__module__', 'dex_connectors.mixpanel.connection')


def new_connection_with_operations(
    track_event: Mutation[TrackEventInput, EventImportResult],
    export_events: Query[ExportEventsInput, ExportedEvents],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, track_event, export_events)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.mixpanel.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        service_account_username = material.get("service_account_username")
        if not isinstance(service_account_username, str) or not service_account_username:
            raise ValueError("Mixpanel credential service_account_username is required")
        service_account_secret = material.get("service_account_secret")
        if not isinstance(service_account_secret, str) or not service_account_secret:
            raise ValueError("Mixpanel credential service_account_secret is required")
        return Credentials(SecretString(service_account_username), SecretString(service_account_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Mixpanel connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Mixpanel project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Mixpanel project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.mixpanel.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "exportBaseUrl", "projectId", "timeout", "maxResponseBytes"}:
        raise ValueError("Mixpanel configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Mixpanel configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "exportBaseUrl" in value:
        export_base_url = value["exportBaseUrl"]
        if not (isinstance(export_base_url, str)):
            raise ValueError("Mixpanel configuration exportBaseUrl has an invalid type")
        config_arguments['export_base_url'] = export_base_url
    if "projectId" in value:
        project_id = value["projectId"]
        if not (type(project_id) is int):
            raise ValueError("Mixpanel configuration projectId has an invalid type")
        config_arguments['project_id'] = project_id
    else:
        raise ValueError("Mixpanel configuration projectId is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Mixpanel configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Mixpanel configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
