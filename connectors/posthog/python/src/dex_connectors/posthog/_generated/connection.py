# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CAPTURE_EVENT_DEFINITION, LIST_INSIGHTS_DEFINITION
from ..models import CaptureEventInput, CapturedEvent, InsightsPage, ListInsightsInput

class Connection:

    __slots__ = ('_capture_event', '_list_insights', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        capture_event: Mutation[CaptureEventInput, CapturedEvent],
        list_insights: Query[ListInsightsInput, InsightsPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or capture_event is None or list_insights is None:
            raise ValueError("PostHog connection provider and operations are required")
        if capture_event.definition().operation != CAPTURE_EVENT_DEFINITION.operation:
            raise ValueError("PostHog connection operation identity differs")
        if list_insights.definition().operation != LIST_INSIGHTS_DEFINITION.operation:
            raise ValueError("PostHog connection operation identity differs")
        self.reference = reference
        self._capture_event = capture_event
        self._list_insights = list_insights
        self._client = client

    def capture_event(self) -> Mutation[CaptureEventInput, CapturedEvent]:
        return self._capture_event

    def list_insights(self) -> Query[ListInsightsInput, InsightsPage]:
        return self._list_insights

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "posthog.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("PostHog connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.posthog.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("PostHog client is required")
    return Connection(reference, client.capture_event(), client.list_insights(), client)

setattr(new_connection, '__module__', 'dex_connectors.posthog.connection')


def new_connection_with_operations(
    capture_event: Mutation[CaptureEventInput, CapturedEvent],
    list_insights: Query[ListInsightsInput, InsightsPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, capture_event, list_insights)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.posthog.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("PostHog stored credential does not match the connector credential fields")
        if set(material) - set(('project_token', 'personal_api_key')):
            raise ValueError("PostHog stored credential does not match the connector credential fields")
        project_token = material.get("project_token")
        if project_token is not None and not (isinstance(project_token, str)):
            raise ValueError("PostHog credential project_token has an invalid type")
        personal_api_key = material.get("personal_api_key")
        if personal_api_key is not None and not (isinstance(personal_api_key, str)):
            raise ValueError("PostHog credential personal_api_key has an invalid type")
        if not project_token:
            raise ValueError("PostHog credential project_token is required")
        return Credentials(SecretString(project_token), SecretString(personal_api_key or ''))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("PostHog connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("PostHog project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("PostHog project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.posthog.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"captureUrl", "appUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("PostHog configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "captureUrl" in value and value["captureUrl"] != '':
        capture_url = value["captureUrl"]
        if not (isinstance(capture_url, str)):
            raise ValueError("PostHog configuration captureUrl has an invalid type")
        config_arguments['capture_url'] = capture_url
    if "appUrl" in value and value["appUrl"] != '':
        app_url = value["appUrl"]
        if not (isinstance(app_url, str)):
            raise ValueError("PostHog configuration appUrl has an invalid type")
        config_arguments['app_url'] = app_url
    if "timeout" in value and value["timeout"] != '':
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("PostHog configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("PostHog configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
