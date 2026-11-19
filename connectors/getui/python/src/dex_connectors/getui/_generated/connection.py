# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_DAILY_PUSH_REPORT_DEFINITION
from ..models import GetDailyPushReportInput, GetDailyPushReportOutput

class Connection:

    __slots__ = ('_get_daily_push_report', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_daily_push_report: Query[GetDailyPushReportInput, GetDailyPushReportOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_daily_push_report is None:
            raise ValueError("Getui connection provider and operations are required")
        if get_daily_push_report.definition().operation != GET_DAILY_PUSH_REPORT_DEFINITION.operation:
            raise ValueError("Getui connection operation identity differs")
        self.reference = reference
        self._get_daily_push_report = get_daily_push_report
        self._client = client

    def get_daily_push_report(self) -> Query[GetDailyPushReportInput, GetDailyPushReportOutput]:
        return self._get_daily_push_report

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "getui.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Getui connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.getui.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Getui client is required")
    return Connection(reference, client.get_daily_push_report(), client)

setattr(new_connection, '__module__', 'dex_connectors.getui.connection')


def new_connection_with_operations(
    get_daily_push_report: Query[GetDailyPushReportInput, GetDailyPushReportOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_daily_push_report)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.getui.connection')


class CredentialMaterialError(ValueError):
    "Stored Getui credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Getui stored credential does not match the connector credential fields")
        if set(material) - set(('appId', 'appKey', 'masterSecret')):
            raise CredentialMaterialError("Getui stored credential does not match the connector credential fields")
        app_id = material.get("appId")
        if app_id is not None and not (isinstance(app_id, str)):
            raise CredentialMaterialError("Getui credential appId has an invalid type")
        app_key = material.get("appKey")
        if app_key is not None and not (isinstance(app_key, str)):
            raise CredentialMaterialError("Getui credential appKey has an invalid type")
        master_secret = material.get("masterSecret")
        if master_secret is not None and not (isinstance(master_secret, str)):
            raise CredentialMaterialError("Getui credential masterSecret has an invalid type")
        if not app_id:
            raise CredentialMaterialError("Getui credential appId is required")
        if not app_key:
            raise CredentialMaterialError("Getui credential appKey is required")
        if not master_secret:
            raise CredentialMaterialError("Getui credential masterSecret is required")
        return Credentials(app_id, SecretString(app_key), SecretString(master_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Getui connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Getui project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Getui project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.getui.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("Getui configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Getui configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Getui configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
