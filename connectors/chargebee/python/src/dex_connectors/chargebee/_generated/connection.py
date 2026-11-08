# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_SUBSCRIPTION_DEFINITION
from ..models import GetSubscriptionInput, SubscriptionStatus

class Connection:

    __slots__ = ('_get_subscription', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_subscription: Query[GetSubscriptionInput, SubscriptionStatus],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_subscription is None:
            raise ValueError("Chargebee connection provider and operations are required")
        if get_subscription.definition().operation != GET_SUBSCRIPTION_DEFINITION.operation:
            raise ValueError("Chargebee connection operation identity differs")
        self.reference = reference
        self._get_subscription = get_subscription
        self._client = client

    def get_subscription(self) -> Query[GetSubscriptionInput, SubscriptionStatus]:
        return self._get_subscription

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "chargebee.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Chargebee connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.chargebee.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Chargebee client is required")
    return Connection(reference, client.get_subscription(), client)

setattr(new_connection, '__module__', 'dex_connectors.chargebee.connection')


def new_connection_with_operations(
    get_subscription: Query[GetSubscriptionInput, SubscriptionStatus],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_subscription)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.chargebee.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Chargebee stored credential does not match the connector credential fields")
        if set(material) - set(('api_key',)):
            raise ValueError("Chargebee stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Chargebee credential api_key has an invalid type")
        if not api_key:
            raise ValueError("Chargebee credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Chargebee connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Chargebee project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Chargebee project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.chargebee.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Chargebee configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Chargebee configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    else:
        raise ValueError("Chargebee configuration baseUrl is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Chargebee configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Chargebee configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
