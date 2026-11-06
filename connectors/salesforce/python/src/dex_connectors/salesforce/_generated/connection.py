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
from .definitions import CONNECTOR_ID, GET_RECORD_DEFINITION, QUERY_RECORDS_DEFINITION, UPDATE_RECORD_DEFINITION, UPSERT_RECORD_BY_EXTERNAL_ID_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import GetRecordInput, QueryRecordsInput, QueryRecordsOutput, Record, UpdateRecordInput, UpdateRecordOutput, UpsertRecordByExternalIDInput, UpsertRecordByExternalIDOutput

class Connection:

    __slots__ = ('_query_records', '_get_record', '_upsert_record_by_external_id', '_update_record', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_records: Query[QueryRecordsInput, QueryRecordsOutput],
        get_record: Query[GetRecordInput, Record],
        upsert_record_by_external_id: Mutation[UpsertRecordByExternalIDInput, UpsertRecordByExternalIDOutput],
        update_record: Mutation[UpdateRecordInput, UpdateRecordOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_records is None or get_record is None or upsert_record_by_external_id is None or update_record is None:
            raise ValueError("Salesforce connection provider and operations are required")
        if query_records.definition().operation != QUERY_RECORDS_DEFINITION.operation:
            raise ValueError("Salesforce connection operation identity differs")
        if get_record.definition().operation != GET_RECORD_DEFINITION.operation:
            raise ValueError("Salesforce connection operation identity differs")
        if upsert_record_by_external_id.definition().operation != UPSERT_RECORD_BY_EXTERNAL_ID_DEFINITION.operation:
            raise ValueError("Salesforce connection operation identity differs")
        if update_record.definition().operation != UPDATE_RECORD_DEFINITION.operation:
            raise ValueError("Salesforce connection operation identity differs")
        self.reference = reference
        self._query_records = query_records
        self._get_record = get_record
        self._upsert_record_by_external_id = upsert_record_by_external_id
        self._update_record = update_record
        self._client = client

    def query_records(self) -> Query[QueryRecordsInput, QueryRecordsOutput]:
        return self._query_records

    def get_record(self) -> Query[GetRecordInput, Record]:
        return self._get_record

    def upsert_record_by_external_id(self) -> Mutation[UpsertRecordByExternalIDInput, UpsertRecordByExternalIDOutput]:
        return self._upsert_record_by_external_id

    def update_record(self) -> Mutation[UpdateRecordInput, UpdateRecordOutput]:
        return self._update_record

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "salesforce.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Salesforce connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.salesforce.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Salesforce client is required")
    return Connection(reference, client.query_records(), client.get_record(), client.upsert_record_by_external_id(), client.update_record(), client)

setattr(new_connection, '__module__', 'dex_connectors.salesforce.connection')


def new_connection_with_operations(
    query_records: Query[QueryRecordsInput, QueryRecordsOutput],
    get_record: Query[GetRecordInput, Record],
    upsert_record_by_external_id: Mutation[UpsertRecordByExternalIDInput, UpsertRecordByExternalIDOutput],
    update_record: Mutation[UpdateRecordInput, UpdateRecordOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_records, get_record, upsert_record_by_external_id, update_record)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.salesforce.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Salesforce stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'instance_url', 'jwt_username', 'jwt_private_key', 'jwt_login_environment', 'auth_method')):
        raise ValueError("Salesforce stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Salesforce credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Salesforce credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Salesforce credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Salesforce credential refresh_token has an invalid type")
    instance_url = material.get("instance_url")
    if instance_url is not None and not (isinstance(instance_url, str)):
        raise ValueError("Salesforce credential instance_url has an invalid type")
    jwt_username = material.get("jwt_username")
    if jwt_username is not None and not (isinstance(jwt_username, str)):
        raise ValueError("Salesforce credential jwt_username has an invalid type")
    jwt_private_key = material.get("jwt_private_key")
    if jwt_private_key is not None and not (isinstance(jwt_private_key, str)):
        raise ValueError("Salesforce credential jwt_private_key has an invalid type")
    jwt_login_environment = material.get("jwt_login_environment")
    if jwt_login_environment is not None and not (isinstance(jwt_login_environment, str)):
        raise ValueError("Salesforce credential jwt_login_environment has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Salesforce credential auth_method is invalid")
    method = selected_method or "salesforce-oauth"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "salesforce-oauth":
        if not oauth_client_id:
            raise ValueError("Salesforce credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Salesforce credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Salesforce credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Salesforce credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["instance_url"] = instance_url or ''
    elif method == "salesforce-sandbox-oauth":
        if not oauth_client_id:
            raise ValueError("Salesforce credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Salesforce credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Salesforce credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Salesforce credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["instance_url"] = instance_url or ''
    elif method == "salesforce-jwt-bearer":
        if not oauth_client_id:
            raise ValueError("Salesforce credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not jwt_username:
            raise ValueError("Salesforce credential jwt_username is required")
        if jwt_username:
            values["jwt_username"] = jwt_username
        if not jwt_private_key and not is_shared:
            raise ValueError("Salesforce credential jwt_private_key is required")
        if jwt_private_key:
            values["jwt_private_key"] = SecretString(jwt_private_key)
        if jwt_login_environment and jwt_login_environment not in ('production', 'sandbox'):
            raise ValueError("Salesforce credential jwt_login_environment is invalid")
        values["jwt_login_environment"] = jwt_login_environment or ''
        if not access_token and is_shared:
            raise ValueError("Salesforce credential access_token is required")
        values["access_token"] = SecretString(access_token or '')
        values["instance_url"] = instance_url or ''
    else:
        raise ValueError("Salesforce credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "salesforce-oauth"
    material['auth_method'] = method
    if method == "salesforce-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if credentials.instance_url:
            material["instance_url"] = credentials.instance_url
    elif method == "salesforce-sandbox-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if credentials.instance_url:
            material["instance_url"] = credentials.instance_url
    elif method == "salesforce-jwt-bearer":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if credentials.jwt_username:
            material["jwt_username"] = credentials.jwt_username
        if (credentials.jwt_private_key.reveal() if credentials.jwt_private_key is not None else ''):
            material["jwt_private_key"] = (credentials.jwt_private_key.reveal() if credentials.jwt_private_key is not None else '')
        if credentials.jwt_login_environment:
            material["jwt_login_environment"] = credentials.jwt_login_environment
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if credentials.instance_url:
            material["instance_url"] = credentials.instance_url
    else:
        raise ValueError("Salesforce credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Salesforce", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Salesforce connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Salesforce project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Salesforce project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.salesforce.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"apiVersion", "maxResponseBytes", "queryBatchSize"}:
        raise ValueError("Salesforce configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "apiVersion" in value:
        api_version = value["apiVersion"]
        if not (isinstance(api_version, str)):
            raise ValueError("Salesforce configuration apiVersion has an invalid type")
        config_arguments['api_version'] = api_version
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Salesforce configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "queryBatchSize" in value:
        query_batch_size = value["queryBatchSize"]
        if not (type(query_batch_size) is int):
            raise ValueError("Salesforce configuration queryBatchSize has an invalid type")
        config_arguments['query_batch_size'] = query_batch_size
    return Config(**config_arguments)
