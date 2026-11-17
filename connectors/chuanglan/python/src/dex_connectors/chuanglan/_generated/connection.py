# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_BALANCE_DEFINITION
from ..models import GetBalanceInput, GetBalanceOutput

class Connection:

    __slots__ = ('_get_balance', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_balance: Query[GetBalanceInput, GetBalanceOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_balance is None:
            raise ValueError("Chuanglan 253 connection provider and operations are required")
        if get_balance.definition().operation != GET_BALANCE_DEFINITION.operation:
            raise ValueError("Chuanglan 253 connection operation identity differs")
        self.reference = reference
        self._get_balance = get_balance
        self._client = client

    def get_balance(self) -> Query[GetBalanceInput, GetBalanceOutput]:
        return self._get_balance

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "chuanglan.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Chuanglan 253 connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.chuanglan.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Chuanglan 253 client is required")
    return Connection(reference, client.get_balance(), client)

setattr(new_connection, '__module__', 'dex_connectors.chuanglan.connection')


def new_connection_with_operations(
    get_balance: Query[GetBalanceInput, GetBalanceOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_balance)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.chuanglan.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Chuanglan 253 stored credential does not match the connector credential fields")
        if set(material) - set(('account', 'password')):
            raise ValueError("Chuanglan 253 stored credential does not match the connector credential fields")
        account = material.get("account")
        if account is not None and not (isinstance(account, str)):
            raise ValueError("Chuanglan 253 credential account has an invalid type")
        password = material.get("password")
        if password is not None and not (isinstance(password, str)):
            raise ValueError("Chuanglan 253 credential password has an invalid type")
        if not account:
            raise ValueError("Chuanglan 253 credential account is required")
        if not password:
            raise ValueError("Chuanglan 253 credential password is required")
        return Credentials(SecretString(account), SecretString(password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Chuanglan 253 connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Chuanglan 253 project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Chuanglan 253 project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.chuanglan.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Chuanglan 253 configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Chuanglan 253 configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Chuanglan 253 configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Chuanglan 253 configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
