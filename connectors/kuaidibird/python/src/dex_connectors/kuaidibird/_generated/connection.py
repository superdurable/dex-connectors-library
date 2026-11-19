# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, QUERY_TRACKING_DEFINITION
from ..models import QueryTrackingInput, QueryTrackingOutput

class Connection:

    __slots__ = ('_query_tracking', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_tracking: Query[QueryTrackingInput, QueryTrackingOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_tracking is None:
            raise ValueError("KuaidiBird connection provider and operations are required")
        if query_tracking.definition().operation != QUERY_TRACKING_DEFINITION.operation:
            raise ValueError("KuaidiBird connection operation identity differs")
        self.reference = reference
        self._query_tracking = query_tracking
        self._client = client

    def query_tracking(self) -> Query[QueryTrackingInput, QueryTrackingOutput]:
        return self._query_tracking

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "kuaidibird.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("KuaidiBird connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.kuaidibird.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("KuaidiBird client is required")
    return Connection(reference, client.query_tracking(), client)

setattr(new_connection, '__module__', 'dex_connectors.kuaidibird.connection')


def new_connection_with_operations(
    query_tracking: Query[QueryTrackingInput, QueryTrackingOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_tracking)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.kuaidibird.connection')


class CredentialMaterialError(ValueError):
    "Stored KuaidiBird credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("KuaidiBird stored credential does not match the connector credential fields")
        if set(material) - set(('business_id', 'api_key')):
            raise CredentialMaterialError("KuaidiBird stored credential does not match the connector credential fields")
        business_id = material.get("business_id")
        if business_id is not None and not (isinstance(business_id, str)):
            raise CredentialMaterialError("KuaidiBird credential business_id has an invalid type")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise CredentialMaterialError("KuaidiBird credential api_key has an invalid type")
        if not business_id:
            raise CredentialMaterialError("KuaidiBird credential business_id is required")
        if not api_key:
            raise CredentialMaterialError("KuaidiBird credential api_key is required")
        return Credentials(SecretString(business_id), SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("KuaidiBird connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("KuaidiBird project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("KuaidiBird project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.kuaidibird.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("KuaidiBird configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("KuaidiBird configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("KuaidiBird configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("KuaidiBird configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
