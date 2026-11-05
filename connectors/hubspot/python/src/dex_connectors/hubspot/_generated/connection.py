# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import (
    CONNECTOR_ID,
    GET_OBJECT_DEFINITION,
    SEARCH_OBJECTS_DEFINITION,
    UPDATE_OBJECT_DEFINITION,
    UPSERT_OBJECT_DEFINITION,
)
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import (
    CRMObject,
    GetObjectInput,
    ObjectPage,
    SearchObjectsInput,
    UpdateObjectInput,
    UpsertObjectInput,
    UpsertedObject,
)


class Connection:
    __slots__ = ("_search_objects", "_get_object", "_upsert_object", "_update_object", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        search_objects: Query[SearchObjectsInput, ObjectPage],
        get_object: Query[GetObjectInput, CRMObject],
        upsert_object: Mutation[UpsertObjectInput, UpsertedObject],
        update_object: Mutation[UpdateObjectInput, CRMObject],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if (
            reference.provider != CONNECTOR_ID
            or search_objects is None
            or get_object is None
            or upsert_object is None
            or update_object is None
        ):
            raise ValueError("HubSpot connection provider and operations are required")
        if search_objects.definition().operation != SEARCH_OBJECTS_DEFINITION.operation:
            raise ValueError("HubSpot connection operation identity differs")
        if get_object.definition().operation != GET_OBJECT_DEFINITION.operation:
            raise ValueError("HubSpot connection operation identity differs")
        if upsert_object.definition().operation != UPSERT_OBJECT_DEFINITION.operation:
            raise ValueError("HubSpot connection operation identity differs")
        if update_object.definition().operation != UPDATE_OBJECT_DEFINITION.operation:
            raise ValueError("HubSpot connection operation identity differs")
        self.reference = reference
        self._search_objects = search_objects
        self._get_object = get_object
        self._upsert_object = upsert_object
        self._update_object = update_object
        self._client = client

    def search_objects(self) -> Query[SearchObjectsInput, ObjectPage]:
        return self._search_objects

    def get_object(self) -> Query[GetObjectInput, CRMObject]:
        return self._get_object

    def upsert_object(self) -> Mutation[UpsertObjectInput, UpsertedObject]:
        return self._upsert_object

    def update_object(self) -> Mutation[UpdateObjectInput, CRMObject]:
        return self._update_object

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "hubspot.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("HubSpot connections cannot be serialized")


setattr(Connection, "__module__", "dex_connectors.hubspot.connection")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("HubSpot client is required")
    return Connection(
        reference,
        client.search_objects(),
        client.get_object(),
        client.upsert_object(),
        client.update_object(),
        client,
    )


setattr(new_connection, "__module__", "dex_connectors.hubspot.connection")


def new_connection_with_operations(
    search_objects: Query[SearchObjectsInput, ObjectPage],
    get_object: Query[GetObjectInput, CRMObject],
    upsert_object: Mutation[UpsertObjectInput, UpsertedObject],
    update_object: Mutation[UpdateObjectInput, CRMObject],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_objects, get_object, upsert_object, update_object)


setattr(new_connection_with_operations, "__module__", "dex_connectors.hubspot.connection")


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("HubSpot stored credential does not match the connector credential fields")
    if set(material) - set(
        ("access_token", "oauth_client_id", "oauth_client_secret", "refresh_token", "auth_method")
    ):
        raise ValueError("HubSpot stored credential does not match the connector credential fields")
    is_shared = shared
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("HubSpot credential access_token has an invalid type")
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("HubSpot credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("HubSpot credential oauth_client_secret has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("HubSpot credential refresh_token has an invalid type")
    selected_method = material.get("auth_method")
    if selected_method is None:
        selected_method = ""
    if not isinstance(selected_method, str):
        raise ValueError("HubSpot credential auth_method is invalid")
    method = selected_method or "private-app-token"
    values: dict[str, Any] = {"auth_method_id": method}
    if method == "private-app-token":
        if not access_token:
            raise ValueError("HubSpot credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
    elif method == "hubspot-oauth":
        if not oauth_client_id:
            raise ValueError("HubSpot credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("HubSpot credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("HubSpot credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("HubSpot credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    else:
        raise ValueError("HubSpot credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "private-app-token"
    material["auth_method"] = method
    if method == "private-app-token":
        if credentials.access_token.reveal() if credentials.access_token is not None else "":
            material["access_token"] = (
                credentials.access_token.reveal() if credentials.access_token is not None else ""
            )
    elif method == "hubspot-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else "":
            material["oauth_client_secret"] = (
                credentials.oauth_client_secret.reveal()
                if credentials.oauth_client_secret is not None
                else ""
            )
        if credentials.access_token.reveal() if credentials.access_token is not None else "":
            material["access_token"] = (
                credentials.access_token.reveal() if credentials.access_token is not None else ""
            )
        if credentials.refresh_token.reveal() if credentials.refresh_token is not None else "":
            material["refresh_token"] = (
                credentials.refresh_token.reveal() if credentials.refresh_token is not None else ""
            )
    else:
        raise ValueError("HubSpot credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(
        self,
        project: LoadedProject,
        key: ConnectionKey,
        http_client: httpx.AsyncClient,
        settings: dict[str, Any],
    ) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials](
            "HubSpot", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings
        )

    async def resolve(self, call: Call) -> Credentials:
        return await self.provider.resolve_with_refresh(call, self.driver)

    async def resolve_with_refresh(
        self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None
    ) -> Credentials:
        return await self.provider.resolve_with_refresh(call, driver or self.driver)

    async def resolve_after_rejection(
        self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None
    ) -> Credentials:
        return await self.provider.resolve_after_rejection(call, driver or self.driver)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("HubSpot connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("HubSpot project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("HubSpot project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(
        config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client
    )
    return new_connection(client, reference)


setattr(new_project_connection, "__module__", "dex_connectors.hubspot.connection")


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes"}:
        raise ValueError("HubSpot configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("HubSpot configuration endpoint has an invalid type")
        config_arguments["endpoint"] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("HubSpot configuration maxResponseBytes has an invalid type")
        config_arguments["max_response_bytes"] = max_response_bytes
    return Config(**config_arguments)
