# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_CURRENT_WEATHER_DEFINITION
from ..models import GetCurrentWeatherInput, GetCurrentWeatherOutput

class Connection:

    __slots__ = ('_get_current_weather', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_current_weather: Query[GetCurrentWeatherInput, GetCurrentWeatherOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_current_weather is None:
            raise ValueError("Caiyun Weather connection provider and operations are required")
        if get_current_weather.definition().operation != GET_CURRENT_WEATHER_DEFINITION.operation:
            raise ValueError("Caiyun Weather connection operation identity differs")
        self.reference = reference
        self._get_current_weather = get_current_weather
        self._client = client

    def get_current_weather(self) -> Query[GetCurrentWeatherInput, GetCurrentWeatherOutput]:
        return self._get_current_weather

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "caiyun.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Caiyun Weather connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.caiyun.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Caiyun Weather client is required")
    return Connection(reference, client.get_current_weather(), client)

setattr(new_connection, '__module__', 'dex_connectors.caiyun.connection')


def new_connection_with_operations(
    get_current_weather: Query[GetCurrentWeatherInput, GetCurrentWeatherOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_current_weather)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.caiyun.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Caiyun Weather stored credential does not match the connector credential fields")
        if set(material) - set(('app_key', 'app_secret')):
            raise ValueError("Caiyun Weather stored credential does not match the connector credential fields")
        app_key = material.get("app_key")
        if app_key is not None and not (isinstance(app_key, str)):
            raise ValueError("Caiyun Weather credential app_key has an invalid type")
        app_secret = material.get("app_secret")
        if app_secret is not None and not (isinstance(app_secret, str)):
            raise ValueError("Caiyun Weather credential app_secret has an invalid type")
        if not app_key:
            raise ValueError("Caiyun Weather credential app_key is required")
        if not app_secret:
            raise ValueError("Caiyun Weather credential app_secret is required")
        return Credentials(SecretString(app_key), SecretString(app_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Caiyun Weather connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Caiyun Weather project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Caiyun Weather project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.caiyun.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Caiyun Weather configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Caiyun Weather configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Caiyun Weather configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Caiyun Weather configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
