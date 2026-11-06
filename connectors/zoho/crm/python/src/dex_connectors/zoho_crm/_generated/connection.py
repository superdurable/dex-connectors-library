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
from .definitions import CONNECTOR_ID, PROVIDER, FIND_RECORDS_DEFINITION, GET_RECORD_DEFINITION, LIST_MODIFIED_RECORDS_DEFINITION, LIST_MODULE_FIELDS_DEFINITION, UPDATE_RECORD_DEFINITION, UPSERT_RECORD_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import FindRecordsInput, FindRecordsOutput, GetRecordInput, ListModifiedRecordsInput, ListModifiedRecordsOutput, ListModuleFieldsInput, ListModuleFieldsOutput, Record, UpdateRecordInput, UpdateRecordOutput, UpsertRecordInput, UpsertRecordOutput

class Connection:

    __slots__ = ('_find_records', '_get_record', '_list_modified_records', '_upsert_record', '_update_record', '_list_module_fields', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        find_records: Query[FindRecordsInput, FindRecordsOutput],
        get_record: Query[GetRecordInput, Record],
        list_modified_records: Query[ListModifiedRecordsInput, ListModifiedRecordsOutput],
        upsert_record: Mutation[UpsertRecordInput, UpsertRecordOutput],
        update_record: Mutation[UpdateRecordInput, UpdateRecordOutput],
        list_module_fields: Query[ListModuleFieldsInput, ListModuleFieldsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or find_records is None or get_record is None or list_modified_records is None or upsert_record is None or update_record is None or list_module_fields is None:
            raise ValueError("Zoho CRM connection provider and operations are required")
        if find_records.definition().operation != FIND_RECORDS_DEFINITION.operation:
            raise ValueError("Zoho CRM connection operation identity differs")
        if get_record.definition().operation != GET_RECORD_DEFINITION.operation:
            raise ValueError("Zoho CRM connection operation identity differs")
        if list_modified_records.definition().operation != LIST_MODIFIED_RECORDS_DEFINITION.operation:
            raise ValueError("Zoho CRM connection operation identity differs")
        if upsert_record.definition().operation != UPSERT_RECORD_DEFINITION.operation:
            raise ValueError("Zoho CRM connection operation identity differs")
        if update_record.definition().operation != UPDATE_RECORD_DEFINITION.operation:
            raise ValueError("Zoho CRM connection operation identity differs")
        if list_module_fields.definition().operation != LIST_MODULE_FIELDS_DEFINITION.operation:
            raise ValueError("Zoho CRM connection operation identity differs")
        self.reference = reference
        self._find_records = find_records
        self._get_record = get_record
        self._list_modified_records = list_modified_records
        self._upsert_record = upsert_record
        self._update_record = update_record
        self._list_module_fields = list_module_fields
        self._client = client

    def find_records(self) -> Query[FindRecordsInput, FindRecordsOutput]:
        return self._find_records

    def get_record(self) -> Query[GetRecordInput, Record]:
        return self._get_record

    def list_modified_records(self) -> Query[ListModifiedRecordsInput, ListModifiedRecordsOutput]:
        return self._list_modified_records

    def upsert_record(self) -> Mutation[UpsertRecordInput, UpsertRecordOutput]:
        return self._upsert_record

    def update_record(self) -> Mutation[UpdateRecordInput, UpdateRecordOutput]:
        return self._update_record

    def list_module_fields(self) -> Query[ListModuleFieldsInput, ListModuleFieldsOutput]:
        return self._list_module_fields

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "zoho_crm.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Zoho CRM connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.zoho_crm.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Zoho CRM client is required")
    return Connection(reference, client.find_records(), client.get_record(), client.list_modified_records(), client.upsert_record(), client.update_record(), client.list_module_fields(), client)

setattr(new_connection, '__module__', 'dex_connectors.zoho_crm.connection')


def new_connection_with_operations(
    find_records: Query[FindRecordsInput, FindRecordsOutput],
    get_record: Query[GetRecordInput, Record],
    list_modified_records: Query[ListModifiedRecordsInput, ListModifiedRecordsOutput],
    upsert_record: Mutation[UpsertRecordInput, UpsertRecordOutput],
    update_record: Mutation[UpdateRecordInput, UpdateRecordOutput],
    list_module_fields: Query[ListModuleFieldsInput, ListModuleFieldsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, find_records, get_record, list_modified_records, upsert_record, update_record, list_module_fields)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.zoho_crm.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Zoho CRM stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'api_domain', 'auth_method')):
        raise ValueError("Zoho CRM stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Zoho CRM credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Zoho CRM credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Zoho CRM credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Zoho CRM credential refresh_token has an invalid type")
    api_domain = material.get("api_domain")
    if api_domain is not None and not (isinstance(api_domain, str)):
        raise ValueError("Zoho CRM credential api_domain has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Zoho CRM credential auth_method is invalid")
    method = selected_method or "zoho-us-oauth"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "zoho-us-oauth":
        if not oauth_client_id:
            raise ValueError("Zoho CRM credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Zoho CRM credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Zoho CRM credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Zoho CRM credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["api_domain"] = api_domain or ''
    elif method == "zoho-eu-oauth":
        if not oauth_client_id:
            raise ValueError("Zoho CRM credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Zoho CRM credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Zoho CRM credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Zoho CRM credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["api_domain"] = api_domain or ''
    elif method == "zoho-in-oauth":
        if not oauth_client_id:
            raise ValueError("Zoho CRM credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Zoho CRM credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Zoho CRM credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Zoho CRM credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["api_domain"] = api_domain or ''
    elif method == "zoho-au-oauth":
        if not oauth_client_id:
            raise ValueError("Zoho CRM credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Zoho CRM credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Zoho CRM credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Zoho CRM credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["api_domain"] = api_domain or ''
    elif method == "zoho-jp-oauth":
        if not oauth_client_id:
            raise ValueError("Zoho CRM credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Zoho CRM credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Zoho CRM credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Zoho CRM credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["api_domain"] = api_domain or ''
    elif method == "zoho-ca-oauth":
        if not oauth_client_id:
            raise ValueError("Zoho CRM credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Zoho CRM credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Zoho CRM credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Zoho CRM credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["api_domain"] = api_domain or ''
    elif method == "zoho-sa-oauth":
        if not oauth_client_id:
            raise ValueError("Zoho CRM credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Zoho CRM credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Zoho CRM credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Zoho CRM credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["api_domain"] = api_domain or ''
    else:
        raise ValueError("Zoho CRM credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "zoho-us-oauth"
    material['auth_method'] = method
    if method == "zoho-us-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if credentials.api_domain:
            material["api_domain"] = credentials.api_domain
    elif method == "zoho-eu-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if credentials.api_domain:
            material["api_domain"] = credentials.api_domain
    elif method == "zoho-in-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if credentials.api_domain:
            material["api_domain"] = credentials.api_domain
    elif method == "zoho-au-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if credentials.api_domain:
            material["api_domain"] = credentials.api_domain
    elif method == "zoho-jp-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if credentials.api_domain:
            material["api_domain"] = credentials.api_domain
    elif method == "zoho-ca-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if credentials.api_domain:
            material["api_domain"] = credentials.api_domain
    elif method == "zoho-sa-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if credentials.api_domain:
            material["api_domain"] = credentials.api_domain
    else:
        raise ValueError("Zoho CRM credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Zoho CRM", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Zoho CRM connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Zoho CRM project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Zoho CRM project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.zoho_crm.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes"}:
        raise ValueError("Zoho CRM configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Zoho CRM configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
