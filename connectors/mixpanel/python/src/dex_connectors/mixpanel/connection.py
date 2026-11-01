# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mixpanel connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    EXPORT_EVENTS_DEFINITION,
    TRACK_EVENT_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import EventImportResult, ExportedEvents, ExportEventsInput, TrackEventInput


class Connection:
    """Nonserializable Mixpanel operation bindings for a named project connection."""

    __slots__ = ("_track_event", "_export_events", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        track_event: Mutation[TrackEventInput, EventImportResult],
        export_events: Query[ExportEventsInput, ExportedEvents],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or track_event is None or export_events is None:
            raise ValueError("Mixpanel connection provider and operations are required")
        if (
            track_event.definition().operation != TRACK_EVENT_DEFINITION.operation
            or export_events.definition().operation != EXPORT_EVENTS_DEFINITION.operation
        ):
            raise ValueError("Mixpanel connection operation identity differs")
        self.reference = reference
        self._track_event = track_event
        self._export_events = export_events
        self._client = client

    def track_event(self) -> Mutation[TrackEventInput, EventImportResult]:
        """Return the event import Mutation used by an application Step."""
        return self._track_event

    def export_events(self) -> Query[ExportEventsInput, ExportedEvents]:
        """Return the raw event export Query used by an application Step."""
        return self._export_events

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "mixpanel.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Mixpanel connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Mixpanel client is required")
    return Connection(reference, client.track_event(), client.export_events(), client)


def new_connection_with_operations(
    track_event: Mutation[TrackEventInput, EventImportResult],
    export_events: Query[ExportEventsInput, ExportedEvents],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, track_event, export_events)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        username = material.get("service_account_username")
        secret = material.get("service_account_secret")
        if not isinstance(username, str) or not username or not isinstance(secret, str) or not secret:
            raise ValueError("Mixpanel service account username and secret are required")
        return Credentials(SecretString(username), SecretString(secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current service account per call."""
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


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "exportBaseUrl", "projectId", "timeout", "maxResponseBytes"}:
        raise ValueError("Mixpanel configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    export_base_url = value.get("exportBaseUrl", "")
    project_id = value.get("projectId", 0)
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str) or not isinstance(export_base_url, str):
        raise ValueError("Mixpanel configuration base URLs must be strings")
    if any(type(number) is not int for number in (project_id, timeout, max_response_bytes)):
        raise ValueError("Mixpanel configuration projectId, timeout, and maxResponseBytes must be integers")
    return Config(project_id, base_url, export_base_url, timeout / 1_000_000_000, max_response_bytes)
