# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_PROPERTY_DEFINITION, LIST_AUDIENCES_DEFINITION, RUN_REPORT_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AudiencesPage, GetPropertyInput, ListAudiencesInput, Property, ReportPage, RunReportInput

class Connection:

    __slots__ = ('_run_report', '_get_property', '_list_audiences', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        run_report: Query[RunReportInput, ReportPage],
        get_property: Query[GetPropertyInput, Property],
        list_audiences: Query[ListAudiencesInput, AudiencesPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or run_report is None or get_property is None or list_audiences is None:
            raise ValueError("Google Analytics 4 connection provider and operations are required")
        if run_report.definition().operation != RUN_REPORT_DEFINITION.operation:
            raise ValueError("Google Analytics 4 connection operation identity differs")
        if get_property.definition().operation != GET_PROPERTY_DEFINITION.operation:
            raise ValueError("Google Analytics 4 connection operation identity differs")
        if list_audiences.definition().operation != LIST_AUDIENCES_DEFINITION.operation:
            raise ValueError("Google Analytics 4 connection operation identity differs")
        self.reference = reference
        self._run_report = run_report
        self._get_property = get_property
        self._list_audiences = list_audiences
        self._client = client

    def run_report(self) -> Query[RunReportInput, ReportPage]:
        return self._run_report

    def get_property(self) -> Query[GetPropertyInput, Property]:
        return self._get_property

    def list_audiences(self) -> Query[ListAudiencesInput, AudiencesPage]:
        return self._list_audiences

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "google_analytics.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Google Analytics 4 connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.google_analytics.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Google Analytics 4 client is required")
    return Connection(reference, client.run_report(), client.get_property(), client.list_audiences(), client)

setattr(new_connection, '__module__', 'dex_connectors.google_analytics.connection')


def new_connection_with_operations(
    run_report: Query[RunReportInput, ReportPage],
    get_property: Query[GetPropertyInput, Property],
    list_audiences: Query[ListAudiencesInput, AudiencesPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, run_report, get_property, list_audiences)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.google_analytics.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Google Analytics 4 stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
        raise ValueError("Google Analytics 4 stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Google Analytics 4 credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Google Analytics 4 credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Google Analytics 4 credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Google Analytics 4 credential refresh_token has an invalid type")
    if not oauth_client_id:
        raise ValueError("Google Analytics 4 credential oauth_client_id is required")
    if not oauth_client_secret and not is_shared:
        raise ValueError("Google Analytics 4 credential oauth_client_secret is required")
    if not access_token:
        raise ValueError("Google Analytics 4 credential access_token is required")
    if not refresh_token and not is_shared:
        raise ValueError("Google Analytics 4 credential refresh_token is required")
    return Credentials(oauth_client_id, SecretString(oauth_client_secret or ''), SecretString(access_token), SecretString(refresh_token or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.oauth_client_id:
        material["oauth_client_id"] = credentials.oauth_client_id
    if credentials.oauth_client_secret.reveal():
        material["oauth_client_secret"] = credentials.oauth_client_secret.reveal()
    if credentials.access_token.reveal():
        material["access_token"] = credentials.access_token.reveal()
    if credentials.refresh_token.reveal():
        material["refresh_token"] = credentials.refresh_token.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Google Analytics 4", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

    async def resolve(self, call: Call) -> Credentials:
        return await self.provider.resolve_with_refresh(call, self.driver)

    async def resolve_with_refresh(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_with_refresh(call, driver or self.driver)

    async def resolve_after_rejection(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_after_rejection(call, driver or self.driver)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Google Analytics 4 connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Google Analytics 4 project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Google Analytics 4 project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.google_analytics.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"dataBaseUrl", "adminBaseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Google Analytics 4 configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "dataBaseUrl" in value:
        data_base_url = value["dataBaseUrl"]
        if not (isinstance(data_base_url, str)):
            raise ValueError("Google Analytics 4 configuration dataBaseUrl has an invalid type")
        config_arguments['data_base_url'] = data_base_url
    if "adminBaseUrl" in value:
        admin_base_url = value["adminBaseUrl"]
        if not (isinstance(admin_base_url, str)):
            raise ValueError("Google Analytics 4 configuration adminBaseUrl has an invalid type")
        config_arguments['admin_base_url'] = admin_base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Google Analytics 4 configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Google Analytics 4 configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
