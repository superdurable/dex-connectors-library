# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_PROJECT_API_SUMMARY_DEFINITION
from ..models import GetProjectAPISummaryInput, GetProjectAPISummaryOutput

class Connection:

    __slots__ = ('_get_project_api_summary', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_project_api_summary: Query[GetProjectAPISummaryInput, GetProjectAPISummaryOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_project_api_summary is None:
            raise ValueError("Apifox connection provider and operations are required")
        if get_project_api_summary.definition().operation != GET_PROJECT_API_SUMMARY_DEFINITION.operation:
            raise ValueError("Apifox connection operation identity differs")
        self.reference = reference
        self._get_project_api_summary = get_project_api_summary
        self._client = client

    def get_project_api_summary(self) -> Query[GetProjectAPISummaryInput, GetProjectAPISummaryOutput]:
        return self._get_project_api_summary

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "apifox.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Apifox connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.apifox.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Apifox client is required")
    return Connection(reference, client.get_project_api_summary(), client)

setattr(new_connection, '__module__', 'dex_connectors.apifox.connection')


def new_connection_with_operations(
    get_project_api_summary: Query[GetProjectAPISummaryInput, GetProjectAPISummaryOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_project_api_summary)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.apifox.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Apifox stored credential does not match the connector credential fields")
        if set(material) - set(('accessToken',)):
            raise ValueError("Apifox stored credential does not match the connector credential fields")
        access_token = material.get("accessToken")
        if access_token is not None and not (isinstance(access_token, str)):
            raise ValueError("Apifox credential accessToken has an invalid type")
        if not access_token:
            raise ValueError("Apifox credential accessToken is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Apifox connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Apifox project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Apifox project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.apifox.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("Apifox configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Apifox configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Apifox configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
