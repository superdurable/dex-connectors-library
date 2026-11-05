# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any
from re import search
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, PROVIDER, GET_THING_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import GetThingInput, GetThingOutput

def _is_absolute_credential_url(value: str) -> bool:
    if value != value.strip() or any(ord(character) < 32 or ord(character) == 127 for character in value) or search(r'%(?![0-9a-fA-F]{2})', value):
        return False
    try:
        parsed = urlsplit(value)
        return bool(parsed.scheme and parsed.hostname and not any(character.isspace() for character in parsed.netloc))
    except ValueError:
        return False

class Connection:

    __slots__ = ('_get_thing', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_thing: Query[GetThingInput, GetThingOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or get_thing is None:
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


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Multi Auth Fixture stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'webhook_signing_key', 'service_account_key', 'delegated_user', 'session_note', 'environment', 'login_url', 'request_count', 'wait_duration', 'labels', 'headers', 'optional_count', 'optional_note', 'tenant_id', 'client_id', 'client_secret', 'auth_method')):
        raise ValueError("Multi Auth Fixture stored credential does not match the connector credential fields")
    is_shared = shared
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
    webhook_signing_key = material.get("webhook_signing_key")
    if webhook_signing_key is not None and not (isinstance(webhook_signing_key, str)):
        raise ValueError("Multi Auth Fixture credential webhook_signing_key has an invalid type")
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
    request_count = material.get("request_count")
    if request_count is not None and not (type(request_count) is int):
        raise ValueError("Multi Auth Fixture credential request_count has an invalid type")
    wait_duration = material.get("wait_duration")
    if wait_duration is not None and not (type(wait_duration) is int):
        raise ValueError("Multi Auth Fixture credential wait_duration has an invalid type")
    labels = material.get("labels")
    if labels is not None and not (isinstance(labels, list) and all(item is None or isinstance(item, str) for item in labels)):
        raise ValueError("Multi Auth Fixture credential labels has an invalid type")
    headers = material.get("headers")
    if headers is not None and not (isinstance(headers, dict) and all(isinstance(key, str) and (item is None or isinstance(item, str)) for key, item in headers.items())):
        raise ValueError("Multi Auth Fixture credential headers has an invalid type")
    optional_count = material.get("optional_count")
    if optional_count is not None and not (type(optional_count) is int):
        raise ValueError("Multi Auth Fixture credential optional_count has an invalid type")
    optional_note = material.get("optional_note")
    if optional_note is not None and not (isinstance(optional_note, str)):
        raise ValueError("Multi Auth Fixture credential optional_note has an invalid type")
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
        values["webhook_signing_key"] = SecretString(webhook_signing_key or '')
    elif method == "workspace-service-account":
        if not service_account_key:
            raise ValueError("Multi Auth Fixture credential service_account_key is required")
        if service_account_key:
            values["service_account_key"] = SecretString(service_account_key)
        if not delegated_user:
            raise ValueError("Multi Auth Fixture credential delegated_user is required")
        if delegated_user:
            values["delegated_user"] = delegated_user
        values["session_note"] = session_note or ''
        if environment and environment not in ('production', 'sandbox'):
            raise ValueError("Multi Auth Fixture credential environment is invalid")
        values["environment"] = environment or ''
        if login_url and not _is_absolute_credential_url(login_url):
            raise ValueError("Multi Auth Fixture credential login_url must be an absolute URL")
        values["login_url"] = login_url or ''
    elif method == "typed":
        if (request_count is None or request_count <= 0):
            raise ValueError("Multi Auth Fixture credential request_count must be positive")
        if request_count is not None and request_count < 0:
            raise ValueError("Multi Auth Fixture credential request_count cannot be negative")
        if request_count:
            values["request_count"] = request_count
        if (wait_duration is None or wait_duration <= 0):
            raise ValueError("Multi Auth Fixture credential wait_duration must be positive")
        if wait_duration is not None and wait_duration < 0:
            raise ValueError("Multi Auth Fixture credential wait_duration cannot be negative")
        if wait_duration:
            values["wait_duration"] = wait_duration
        if labels:
            values["labels"] = [item or '' for item in labels]
        if headers:
            values["headers"] = {key: item or '' for key, item in headers.items()}
        if optional_count is not None and optional_count < 0:
            raise ValueError("Multi Auth Fixture credential optional_count cannot be negative")
        if optional_count:
            values["optional_count"] = optional_count
    elif method == "optional-only":
        values["optional_note"] = optional_note or ''
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
        values["access_token"] = SecretString(access_token or '')
    else:
        raise ValueError("Multi Auth Fixture credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "google-oauth"
    material['auth_method'] = method
    if method == "google-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if (credentials.webhook_signing_key.reveal() if credentials.webhook_signing_key is not None else ''):
            material["webhook_signing_key"] = (credentials.webhook_signing_key.reveal() if credentials.webhook_signing_key is not None else '')
    elif method == "workspace-service-account":
        if (credentials.service_account_key.reveal() if credentials.service_account_key is not None else ''):
            material["service_account_key"] = (credentials.service_account_key.reveal() if credentials.service_account_key is not None else '')
        if credentials.delegated_user:
            material["delegated_user"] = credentials.delegated_user
        if credentials.session_note:
            material["session_note"] = credentials.session_note
        if credentials.environment:
            material["environment"] = credentials.environment
        if credentials.login_url:
            material["login_url"] = credentials.login_url
    elif method == "typed":
        if credentials.request_count:
            material["request_count"] = credentials.request_count
        if credentials.wait_duration:
            material["wait_duration"] = credentials.wait_duration
        if credentials.labels:
            material["labels"] = credentials.labels
        if credentials.headers:
            material["headers"] = credentials.headers
        if credentials.optional_count:
            material["optional_count"] = credentials.optional_count
    elif method == "optional-only":
        if credentials.optional_note:
            material["optional_note"] = credentials.optional_note
    elif method == "app-only":
        if credentials.tenant_id:
            material["tenant_id"] = credentials.tenant_id
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if (credentials.client_secret.reveal() if credentials.client_secret is not None else ''):
            material["client_secret"] = (credentials.client_secret.reveal() if credentials.client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
    else:
        raise ValueError("Multi Auth Fixture credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Multi Auth Fixture", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Multi Auth Fixture connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Multi Auth Fixture project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Multi Auth Fixture project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
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
