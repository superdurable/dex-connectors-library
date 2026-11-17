# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_BEHAVIOR_SUMMARY_DEFINITION
from ..models import GetBehaviorSummaryInput, GetBehaviorSummaryOutput

class Connection:

    __slots__ = ('_get_behavior_summary', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_behavior_summary: Query[GetBehaviorSummaryInput, GetBehaviorSummaryOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_behavior_summary is None:
            raise ValueError("Douyin connection provider and operations are required")
        if get_behavior_summary.definition().operation != GET_BEHAVIOR_SUMMARY_DEFINITION.operation:
            raise ValueError("Douyin connection operation identity differs")
        self.reference = reference
        self._get_behavior_summary = get_behavior_summary
        self._client = client

    def get_behavior_summary(self) -> Query[GetBehaviorSummaryInput, GetBehaviorSummaryOutput]:
        return self._get_behavior_summary

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "douyin.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Douyin connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.douyin.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Douyin client is required")
    return Connection(reference, client.get_behavior_summary(), client)

setattr(new_connection, '__module__', 'dex_connectors.douyin.connection')


def new_connection_with_operations(
    get_behavior_summary: Query[GetBehaviorSummaryInput, GetBehaviorSummaryOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_behavior_summary)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.douyin.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Douyin stored credential does not match the connector credential fields")
        if set(material) - set(('clientKey', 'clientSecret')):
            raise ValueError("Douyin stored credential does not match the connector credential fields")
        client_key = material.get("clientKey")
        if client_key is not None and not (isinstance(client_key, str)):
            raise ValueError("Douyin credential clientKey has an invalid type")
        client_secret = material.get("clientSecret")
        if client_secret is not None and not (isinstance(client_secret, str)):
            raise ValueError("Douyin credential clientSecret has an invalid type")
        if not client_key:
            raise ValueError("Douyin credential clientKey is required")
        if not client_secret:
            raise ValueError("Douyin credential clientSecret is required")
        return Credentials(client_key, SecretString(client_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Douyin connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Douyin project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Douyin project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.douyin.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("Douyin configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Douyin configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Douyin configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
