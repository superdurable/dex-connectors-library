# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_RECORD_DEFINITION, LIST_RECORDS_DEFINITION, UPDATE_RECORDS_DEFINITION, UPSERT_RECORDS_DEFINITION
from ..models import GetRecordInput, ListRecordsInput, Record, RecordPage, UpdateRecordsInput, UpdatedRecords, UpsertRecordsInput, UpsertedRecords

class Connection:

    __slots__ = ('_list_records', '_get_record', '_upsert_records', '_update_records', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_records: Query[ListRecordsInput, RecordPage],
        get_record: Query[GetRecordInput, Record],
        upsert_records: Mutation[UpsertRecordsInput, UpsertedRecords],
        update_records: Mutation[UpdateRecordsInput, UpdatedRecords],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_records is None or get_record is None or upsert_records is None or update_records is None:
            raise ValueError("Airtable connection provider and operations are required")
        if list_records.definition().operation != LIST_RECORDS_DEFINITION.operation:
            raise ValueError("Airtable connection operation identity differs")
        if get_record.definition().operation != GET_RECORD_DEFINITION.operation:
            raise ValueError("Airtable connection operation identity differs")
        if upsert_records.definition().operation != UPSERT_RECORDS_DEFINITION.operation:
            raise ValueError("Airtable connection operation identity differs")
        if update_records.definition().operation != UPDATE_RECORDS_DEFINITION.operation:
            raise ValueError("Airtable connection operation identity differs")
        self.reference = reference
        self._list_records = list_records
        self._get_record = get_record
        self._upsert_records = upsert_records
        self._update_records = update_records
        self._client = client

    def list_records(self) -> Query[ListRecordsInput, RecordPage]:
        return self._list_records

    def get_record(self) -> Query[GetRecordInput, Record]:
        return self._get_record

    def upsert_records(self) -> Mutation[UpsertRecordsInput, UpsertedRecords]:
        return self._upsert_records

    def update_records(self) -> Mutation[UpdateRecordsInput, UpdatedRecords]:
        return self._update_records

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "airtable.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Airtable connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.airtable.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Airtable client is required")
    return Connection(reference, client.list_records(), client.get_record(), client.upsert_records(), client.update_records(), client)

setattr(new_connection, '__module__', 'dex_connectors.airtable.connection')


def new_connection_with_operations(
    list_records: Query[ListRecordsInput, RecordPage],
    get_record: Query[GetRecordInput, Record],
    upsert_records: Mutation[UpsertRecordsInput, UpsertedRecords],
    update_records: Mutation[UpdateRecordsInput, UpdatedRecords],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_records, get_record, upsert_records, update_records)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.airtable.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        personal_access_token = material.get("personal_access_token")
        if not isinstance(personal_access_token, str) or not personal_access_token:
            raise ValueError("Airtable credential personal_access_token is required")
        return Credentials(SecretString(personal_access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Airtable connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Airtable project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Airtable project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.airtable.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes"}:
        raise ValueError("Airtable configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Airtable configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Airtable configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
