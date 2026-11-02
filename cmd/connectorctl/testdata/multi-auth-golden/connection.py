# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_THING_DEFINITION
from ..models import GetThingInput, GetThingOutput

class Connection:

    __slots__ = ('_get_thing', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_thing: Query[GetThingInput, GetThingOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_thing is None:
            raise ValueError("Multi Auth Fixture connection provider and operations are required")
        if get_thing.definition().operation != GET_THING_DEFINITION.operation:
            raise ValueError("Multi Auth Fixture connection operation identity differs")
        self.reference = reference
        self._get_thing = get_thing
        self._client = client

    def get_thing(self) -> Query[GetThingInput, GetThingOutput]:
        return self._get_thing

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "multi_auth_fixture.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Multi Auth Fixture connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.multi_auth_fixture.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Multi Auth Fixture client is required")
    return Connection(reference, client.get_thing(), client)

setattr(new_connection, '__module__', 'dex_connectors.multi_auth_fixture.connection')


def new_connection_with_operations(
    get_thing: Query[GetThingInput, GetThingOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_thing)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.multi_auth_fixture.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey, auth_method_id: str) -> None:
        self.project = project
        self.key = key
        self.auth_method_id = auth_method_id

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        selected_method = material.get('auth_method', '')
        if not isinstance(selected_method, str):
            raise ValueError("Multi Auth Fixture credential auth_method is invalid")
        method = selected_method or self.auth_method_id or "google-oauth"
        values: dict[str, Any] = {'auth_method_id': method}
        if method == "google-oauth":
            oauth_client_id = material.get("oauth_client_id")
            if oauth_client_id is not None and not isinstance(oauth_client_id, str):
                raise ValueError("Multi Auth Fixture credential oauth_client_id has an invalid type")
            if not oauth_client_id:
                raise ValueError("Multi Auth Fixture credential oauth_client_id is required")
            if oauth_client_id:
                values["oauth_client_id"] = oauth_client_id
            oauth_client_secret = material.get("oauth_client_secret")
            if oauth_client_secret is not None and not isinstance(oauth_client_secret, str):
                raise ValueError("Multi Auth Fixture credential oauth_client_secret has an invalid type")
            if not oauth_client_secret:
                raise ValueError("Multi Auth Fixture credential oauth_client_secret is required")
            if oauth_client_secret:
                values["oauth_client_secret"] = SecretString(oauth_client_secret)
            access_token = material.get("access_token")
            if access_token is not None and not isinstance(access_token, str):
                raise ValueError("Multi Auth Fixture credential access_token has an invalid type")
            if not access_token:
                raise ValueError("Multi Auth Fixture credential access_token is required")
            if access_token:
                values["access_token"] = SecretString(access_token)
            refresh_token = material.get("refresh_token")
            if refresh_token is not None and not isinstance(refresh_token, str):
                raise ValueError("Multi Auth Fixture credential refresh_token has an invalid type")
            if not refresh_token:
                raise ValueError("Multi Auth Fixture credential refresh_token is required")
            if refresh_token:
                values["refresh_token"] = SecretString(refresh_token)
        elif method == "workspace-service-account":
            service_account_key = material.get("service_account_key")
            if service_account_key is not None and not isinstance(service_account_key, str):
                raise ValueError("Multi Auth Fixture credential service_account_key has an invalid type")
            if not service_account_key:
                raise ValueError("Multi Auth Fixture credential service_account_key is required")
            if service_account_key:
                values["service_account_key"] = SecretString(service_account_key)
            delegated_user = material.get("delegated_user")
            if delegated_user is not None and not isinstance(delegated_user, str):
                raise ValueError("Multi Auth Fixture credential delegated_user has an invalid type")
            if not delegated_user:
                raise ValueError("Multi Auth Fixture credential delegated_user is required")
            if delegated_user:
                values["delegated_user"] = delegated_user
        else:
            raise ValueError("Multi Auth Fixture credential auth_method is invalid")
        return Credentials(**values)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Multi Auth Fixture connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Multi Auth Fixture project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Multi Auth Fixture project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key, matching[0].auth_method_id), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.multi_auth_fixture.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"workspaceId"}:
        raise ValueError("Multi Auth Fixture configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "workspaceId" in value:
        workspace_id = value["workspaceId"]
        if not (isinstance(workspace_id, str)):
            raise ValueError("Multi Auth Fixture configuration workspaceId has an invalid type")
        config_arguments['workspace_id'] = workspace_id
    else:
        config_arguments['workspace_id'] = ''
    return Config(**config_arguments)
