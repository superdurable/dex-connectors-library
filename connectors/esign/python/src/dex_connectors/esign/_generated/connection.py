# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_SIGN_FLOW_STATUS_DEFINITION
from ..models import GetSignFlowStatusInput, GetSignFlowStatusOutput

class Connection:

    __slots__ = ('_get_sign_flow_status', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_sign_flow_status: Query[GetSignFlowStatusInput, GetSignFlowStatusOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_sign_flow_status is None:
            raise ValueError("eSign connection provider and operations are required")
        if get_sign_flow_status.definition().operation != GET_SIGN_FLOW_STATUS_DEFINITION.operation:
            raise ValueError("eSign connection operation identity differs")
        self.reference = reference
        self._get_sign_flow_status = get_sign_flow_status
        self._client = client

    def get_sign_flow_status(self) -> Query[GetSignFlowStatusInput, GetSignFlowStatusOutput]:
        return self._get_sign_flow_status

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "esign.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("eSign connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.esign.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("eSign client is required")
    return Connection(reference, client.get_sign_flow_status(), client)

setattr(new_connection, '__module__', 'dex_connectors.esign.connection')


def new_connection_with_operations(
    get_sign_flow_status: Query[GetSignFlowStatusInput, GetSignFlowStatusOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_sign_flow_status)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.esign.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("eSign stored credential does not match the connector credential fields")
        if set(material) - set(('app_id', 'app_secret')):
            raise ValueError("eSign stored credential does not match the connector credential fields")
        app_id = material.get("app_id")
        if app_id is not None and not (isinstance(app_id, str)):
            raise ValueError("eSign credential app_id has an invalid type")
        app_secret = material.get("app_secret")
        if app_secret is not None and not (isinstance(app_secret, str)):
            raise ValueError("eSign credential app_secret has an invalid type")
        if not app_id:
            raise ValueError("eSign credential app_id is required")
        if not app_secret:
            raise ValueError("eSign credential app_secret is required")
        return Credentials(app_id, SecretString(app_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("eSign connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("eSign project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("eSign project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.esign.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("eSign configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("eSign configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("eSign configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
