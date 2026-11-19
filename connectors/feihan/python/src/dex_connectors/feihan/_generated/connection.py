# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, QUERY_RECHARGE_ORDER_DEFINITION
from ..models import QueryRechargeOrderInput, RechargeOrderResult

class Connection:

    __slots__ = ('_query_recharge_order', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_recharge_order: Query[QueryRechargeOrderInput, RechargeOrderResult],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_recharge_order is None:
            raise ValueError("Feihan connection provider and operations are required")
        if query_recharge_order.definition().operation != QUERY_RECHARGE_ORDER_DEFINITION.operation:
            raise ValueError("Feihan connection operation identity differs")
        self.reference = reference
        self._query_recharge_order = query_recharge_order
        self._client = client

    def query_recharge_order(self) -> Query[QueryRechargeOrderInput, RechargeOrderResult]:
        return self._query_recharge_order

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "feihan.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Feihan connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.feihan.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Feihan client is required")
    return Connection(reference, client.query_recharge_order(), client)

setattr(new_connection, '__module__', 'dex_connectors.feihan.connection')


def new_connection_with_operations(
    query_recharge_order: Query[QueryRechargeOrderInput, RechargeOrderResult],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_recharge_order)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.feihan.connection')


class CredentialMaterialError(ValueError):
    "Stored Feihan credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Feihan stored credential does not match the connector credential fields")
        if set(material) - set(('project_id', 'password', 'key_str')):
            raise CredentialMaterialError("Feihan stored credential does not match the connector credential fields")
        project_id = material.get("project_id")
        if project_id is not None and not (isinstance(project_id, str)):
            raise CredentialMaterialError("Feihan credential project_id has an invalid type")
        password = material.get("password")
        if password is not None and not (isinstance(password, str)):
            raise CredentialMaterialError("Feihan credential password has an invalid type")
        key_str = material.get("key_str")
        if key_str is not None and not (isinstance(key_str, str)):
            raise CredentialMaterialError("Feihan credential key_str has an invalid type")
        if not project_id:
            raise CredentialMaterialError("Feihan credential project_id is required")
        if not password:
            raise CredentialMaterialError("Feihan credential password is required")
        if not key_str:
            raise CredentialMaterialError("Feihan credential key_str is required")
        return Credentials(project_id, SecretString(password), SecretString(key_str))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Feihan connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Feihan project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Feihan project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.feihan.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Feihan configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Feihan configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Feihan configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Feihan configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
