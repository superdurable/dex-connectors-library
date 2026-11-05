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
from .definitions import CONNECTOR_ID, PROVIDER, GET_THING_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import GetThingInput, GetThingOutput

class Connection:

    __slots__ = ('_get_thing', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_thing: Query[GetThingInput, GetThingOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or get_thing is None:
            raise ValueError("Token Grants Fixture connection provider and operations are required")
        if get_thing.definition().operation != GET_THING_DEFINITION.operation:
            raise ValueError("Token Grants Fixture connection operation identity differs")
        self.reference = reference
        self._get_thing = get_thing
        self._client = client

    def get_thing(self) -> Query[GetThingInput, GetThingOutput]:
        return self._get_thing

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "jwt_bearer_fixture.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Token Grants Fixture connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.jwt_bearer_fixture.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Token Grants Fixture client is required")
    return Connection(reference, client.get_thing(), client)

setattr(new_connection, '__module__', 'dex_connectors.jwt_bearer_fixture.connection')


def new_connection_with_operations(
    get_thing: Query[GetThingInput, GetThingOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_thing)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.jwt_bearer_fixture.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Token Grants Fixture stored credential does not match the connector credential fields")
    if set(material) - set(('client_id', 'username', 'private_key', 'login_environment', 'access_token', 'instance_url', 'auth_method')):
        raise ValueError("Token Grants Fixture stored credential does not match the connector credential fields")
    is_shared = shared
    client_id = material.get("client_id")
    if client_id is not None and not (isinstance(client_id, str)):
        raise ValueError("Token Grants Fixture credential client_id has an invalid type")
    username = material.get("username")
    if username is not None and not (isinstance(username, str)):
        raise ValueError("Token Grants Fixture credential username has an invalid type")
    private_key = material.get("private_key")
    if private_key is not None and not (isinstance(private_key, str)):
        raise ValueError("Token Grants Fixture credential private_key has an invalid type")
    login_environment = material.get("login_environment")
    if login_environment is not None and not (isinstance(login_environment, str)):
        raise ValueError("Token Grants Fixture credential login_environment has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Token Grants Fixture credential access_token has an invalid type")
    instance_url = material.get("instance_url")
    if instance_url is not None and not (isinstance(instance_url, str)):
        raise ValueError("Token Grants Fixture credential instance_url has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Token Grants Fixture credential auth_method is invalid")
    method = selected_method or "jwt-bearer"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "jwt-bearer":
        if not client_id:
            raise ValueError("Token Grants Fixture credential client_id is required")
        if client_id:
            values["client_id"] = client_id
        if not username:
            raise ValueError("Token Grants Fixture credential username is required")
        if username:
            values["username"] = username
        if not private_key and not is_shared:
            raise ValueError("Token Grants Fixture credential private_key is required")
        if private_key:
            values["private_key"] = SecretString(private_key)
        if login_environment and login_environment not in ('production', 'sandbox'):
            raise ValueError("Token Grants Fixture credential login_environment is invalid")
        values["login_environment"] = login_environment or ''
        if not access_token and is_shared:
            raise ValueError("Token Grants Fixture credential access_token is required")
        values["access_token"] = SecretString(access_token or '')
        values["instance_url"] = instance_url or ''
    else:
        raise ValueError("Token Grants Fixture credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "jwt-bearer"
    material['auth_method'] = method
    if method == "jwt-bearer":
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if credentials.username:
            material["username"] = credentials.username
        if (credentials.private_key.reveal() if credentials.private_key is not None else ''):
            material["private_key"] = (credentials.private_key.reveal() if credentials.private_key is not None else '')
        if credentials.login_environment:
            material["login_environment"] = credentials.login_environment
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if credentials.instance_url:
            material["instance_url"] = credentials.instance_url
    else:
        raise ValueError("Token Grants Fixture credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Token Grants Fixture", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Token Grants Fixture connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Token Grants Fixture project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Token Grants Fixture project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.jwt_bearer_fixture.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"tenantId"}:
        raise ValueError("Token Grants Fixture configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "tenantId" in value:
        tenant_id = value["tenantId"]
        if not (isinstance(tenant_id, str)):
            raise ValueError("Token Grants Fixture configuration tenantId has an invalid type")
        config_arguments['tenant_id'] = tenant_id
    else:
        config_arguments['tenant_id'] = ''
    return Config(**config_arguments)
