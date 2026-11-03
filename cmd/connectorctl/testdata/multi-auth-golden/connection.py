# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any
from urllib.parse import urlsplit

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

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Multi Auth Fixture stored credential does not match the connector credential fields")
        if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'service_account_key', 'delegated_user', 'session_note', 'environment', 'login_url', 'tenant_id', 'client_id', 'client_secret', 'auth_method')):
            raise ValueError("Multi Auth Fixture stored credential does not match the connector credential fields")
        is_shared = bool(self.project.shared_connections and self.key in self.project.shared_connections)
        oauth_client_id = material.get("oauth_client_id")
        if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
            raise ValueError("Multi Auth Fixture credential oauth_client_id has an invalid type")
        oauth_client_secret = material.get("oauth_client_secret")
        if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
            raise ValueError("Multi Auth Fixture credential oauth_client_secret has an invalid type")
        access_token = material.get("access_token")
        if access_token is not None and not (isinstance(access_token, str)):
            raise ValueError("Multi Auth Fixture credential access_token has an invalid type")
        refresh_token = material.get("refresh_token")
        if refresh_token is not None and not (isinstance(refresh_token, str)):
            raise ValueError("Multi Auth Fixture credential refresh_token has an invalid type")
        service_account_key = material.get("service_account_key")
        if service_account_key is not None and not (isinstance(service_account_key, str)):
            raise ValueError("Multi Auth Fixture credential service_account_key has an invalid type")
        delegated_user = material.get("delegated_user")
        if delegated_user is not None and not (isinstance(delegated_user, str)):
            raise ValueError("Multi Auth Fixture credential delegated_user has an invalid type")
        session_note = material.get("session_note")
        if session_note is not None and not (isinstance(session_note, str)):
            raise ValueError("Multi Auth Fixture credential session_note has an invalid type")
        environment = material.get("environment")
        if environment is not None and not (isinstance(environment, str)):
            raise ValueError("Multi Auth Fixture credential environment has an invalid type")
        login_url = material.get("login_url")
        if login_url is not None and not (isinstance(login_url, str)):
            raise ValueError("Multi Auth Fixture credential login_url has an invalid type")
        tenant_id = material.get("tenant_id")
        if tenant_id is not None and not (isinstance(tenant_id, str)):
            raise ValueError("Multi Auth Fixture credential tenant_id has an invalid type")
        client_id = material.get("client_id")
        if client_id is not None and not (isinstance(client_id, str)):
            raise ValueError("Multi Auth Fixture credential client_id has an invalid type")
        client_secret = material.get("client_secret")
        if client_secret is not None and not (isinstance(client_secret, str)):
            raise ValueError("Multi Auth Fixture credential client_secret has an invalid type")
        selected_method = material.get('auth_method')
        if selected_method is None:
            selected_method = ''
        if not isinstance(selected_method, str):
            raise ValueError("Multi Auth Fixture credential auth_method is invalid")
        method = selected_method or "google-oauth"
        values: dict[str, Any] = {'auth_method_id': method}
        if method == "google-oauth":
            if not oauth_client_id:
                raise ValueError("Multi Auth Fixture credential oauth_client_id is required")
            if oauth_client_id:
                values["oauth_client_id"] = oauth_client_id
            if not oauth_client_secret and not is_shared:
                raise ValueError("Multi Auth Fixture credential oauth_client_secret is required")
            if oauth_client_secret:
                values["oauth_client_secret"] = SecretString(oauth_client_secret)
            if not access_token:
                raise ValueError("Multi Auth Fixture credential access_token is required")
            if access_token:
                values["access_token"] = SecretString(access_token)
            if not refresh_token and not is_shared:
                raise ValueError("Multi Auth Fixture credential refresh_token is required")
            if refresh_token:
                values["refresh_token"] = SecretString(refresh_token)
        elif method == "workspace-service-account":
            if not service_account_key:
                raise ValueError("Multi Auth Fixture credential service_account_key is required")
            if service_account_key:
                values["service_account_key"] = SecretString(service_account_key)
            if not delegated_user:
                raise ValueError("Multi Auth Fixture credential delegated_user is required")
            if delegated_user:
                values["delegated_user"] = delegated_user
            if session_note:
                values["session_note"] = session_note
            if environment and environment not in ('production', 'sandbox'):
                raise ValueError("Multi Auth Fixture credential environment is invalid")
            if environment:
                values["environment"] = environment
            if login_url and (not urlsplit(login_url).scheme or not urlsplit(login_url).hostname):
                raise ValueError("Multi Auth Fixture credential login_url must be an absolute URL")
            if login_url:
                values["login_url"] = login_url
        elif method == "app-only":
            if not tenant_id:
                raise ValueError("Multi Auth Fixture credential tenant_id is required")
            if tenant_id:
                values["tenant_id"] = tenant_id
            if not client_id:
                raise ValueError("Multi Auth Fixture credential client_id is required")
            if client_id:
                values["client_id"] = client_id
            if not client_secret and not is_shared:
                raise ValueError("Multi Auth Fixture credential client_secret is required")
            if client_secret:
                values["client_secret"] = SecretString(client_secret)
            if not access_token and is_shared:
                raise ValueError("Multi Auth Fixture credential access_token is required")
            if access_token:
                values["access_token"] = SecretString(access_token)
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
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.multi_auth_fixture.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"region", "accountId", "workspaceId"}:
        raise ValueError("Multi Auth Fixture configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "region" in value:
        region = value["region"]
        if not (isinstance(region, str)):
            raise ValueError("Multi Auth Fixture configuration region has an invalid type")
        config_arguments['region'] = region
    else:
        config_arguments['region'] = ''
    if "accountId" in value:
        account_id = value["accountId"]
        if not (isinstance(account_id, str)):
            raise ValueError("Multi Auth Fixture configuration accountId has an invalid type")
        config_arguments['account_id'] = account_id
    else:
        raise ValueError("Multi Auth Fixture configuration accountId is required")
    if "workspaceId" in value:
        workspace_id = value["workspaceId"]
        if not (isinstance(workspace_id, str)):
            raise ValueError("Multi Auth Fixture configuration workspaceId has an invalid type")
        config_arguments['workspace_id'] = workspace_id
    else:
        config_arguments['workspace_id'] = ''
    return Config(**config_arguments)
