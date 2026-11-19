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
            raise ValueError("QWeather connection provider and operations are required")
        if get_current_weather.definition().operation != GET_CURRENT_WEATHER_DEFINITION.operation:
            raise ValueError("QWeather connection operation identity differs")
        self.reference = reference
        self._get_current_weather = get_current_weather
        self._client = client

    def get_current_weather(self) -> Query[GetCurrentWeatherInput, GetCurrentWeatherOutput]:
        return self._get_current_weather

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "qweather.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("QWeather connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.qweather.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("QWeather client is required")
    return Connection(reference, client.get_current_weather(), client)

setattr(new_connection, '__module__', 'dex_connectors.qweather.connection')


def new_connection_with_operations(
    get_current_weather: Query[GetCurrentWeatherInput, GetCurrentWeatherOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_current_weather)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.qweather.connection')


class CredentialMaterialError(ValueError):
    "Stored QWeather credential material fails the connector credential checks, so no retry can succeed."


def is_credential_material_error(error: BaseException) -> bool:
    """Report malformed stored credential material, also behind the SDK provider's decoding ValueError."""
    if isinstance(error, CredentialMaterialError):
        return True
    # Only the SDK decoding wrapper qualifies; other errors may be transient.
    return (
        type(error) is ValueError
        and str(error) in {'project credential decoding failed', 'shared project credential decoding failed'}
        and isinstance(error.__context__, CredentialMaterialError)
    )


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise CredentialMaterialError("QWeather stored credential does not match the connector credential fields")
        if set(material) - set(('api_host', 'api_key')):
            raise CredentialMaterialError("QWeather stored credential does not match the connector credential fields")
        api_host = material.get("api_host")
        if api_host is not None and not (isinstance(api_host, str)):
            raise CredentialMaterialError("QWeather credential api_host has an invalid type")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise CredentialMaterialError("QWeather credential api_key has an invalid type")
        if not api_host:
            raise CredentialMaterialError("QWeather credential api_host is required")
        if not api_key:
            raise CredentialMaterialError("QWeather credential api_key is required")
        return Credentials(SecretString(api_host), SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("QWeather connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("QWeather project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("QWeather project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.qweather.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("QWeather configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("QWeather configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("QWeather configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
