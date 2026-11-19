# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_QUOTAS_DEFINITION
from ..models import ListQuotasInput, QuotaPage

class Connection:

    __slots__ = ('_list_quotas', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_quotas: Query[ListQuotasInput, QuotaPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_quotas is None:
            raise ValueError("Zhihu connection provider and operations are required")
        if list_quotas.definition().operation != LIST_QUOTAS_DEFINITION.operation:
            raise ValueError("Zhihu connection operation identity differs")
        self.reference = reference
        self._list_quotas = list_quotas
        self._client = client

    def list_quotas(self) -> Query[ListQuotasInput, QuotaPage]:
        return self._list_quotas

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "zhihu.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Zhihu connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.zhihu.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Zhihu client is required")
    return Connection(reference, client.list_quotas(), client)

setattr(new_connection, '__module__', 'dex_connectors.zhihu.connection')


def new_connection_with_operations(
    list_quotas: Query[ListQuotasInput, QuotaPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_quotas)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.zhihu.connection')


class CredentialMaterialError(ValueError):
    "Stored Zhihu credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Zhihu stored credential does not match the connector credential fields")
        if set(material) - set(('access_secret',)):
            raise CredentialMaterialError("Zhihu stored credential does not match the connector credential fields")
        access_secret = material.get("access_secret")
        if access_secret is not None and not (isinstance(access_secret, str)):
            raise CredentialMaterialError("Zhihu credential access_secret has an invalid type")
        if not access_secret:
            raise CredentialMaterialError("Zhihu credential access_secret is required")
        return Credentials(SecretString(access_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Zhihu connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Zhihu project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Zhihu project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.zhihu.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Zhihu configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Zhihu configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Zhihu configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Zhihu configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
