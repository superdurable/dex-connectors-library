# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_OBJECT_TEXT_DEFINITION, HEAD_OBJECT_DEFINITION, LIST_OBJECTS_DEFINITION, PUT_OBJECT_DEFINITION
from ..models import GetObjectTextInput, HeadObjectInput, ListObjectsInput, ListObjectsOutput, ObjectMetadata, ObjectText, PutObjectInput, StoredObject

class Connection:

    __slots__ = ('_list_objects', '_head_object', '_get_object_text', '_put_object', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_objects: Query[ListObjectsInput, ListObjectsOutput],
        head_object: Query[HeadObjectInput, ObjectMetadata],
        get_object_text: Query[GetObjectTextInput, ObjectText],
        put_object: Mutation[PutObjectInput, StoredObject],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_objects is None or head_object is None or get_object_text is None or put_object is None:
            raise ValueError("Amazon S3 connection provider and operations are required")
        if list_objects.definition().operation != LIST_OBJECTS_DEFINITION.operation:
            raise ValueError("Amazon S3 connection operation identity differs")
        if head_object.definition().operation != HEAD_OBJECT_DEFINITION.operation:
            raise ValueError("Amazon S3 connection operation identity differs")
        if get_object_text.definition().operation != GET_OBJECT_TEXT_DEFINITION.operation:
            raise ValueError("Amazon S3 connection operation identity differs")
        if put_object.definition().operation != PUT_OBJECT_DEFINITION.operation:
            raise ValueError("Amazon S3 connection operation identity differs")
        self.reference = reference
        self._list_objects = list_objects
        self._head_object = head_object
        self._get_object_text = get_object_text
        self._put_object = put_object
        self._client = client

    def list_objects(self) -> Query[ListObjectsInput, ListObjectsOutput]:
        return self._list_objects

    def head_object(self) -> Query[HeadObjectInput, ObjectMetadata]:
        return self._head_object

    def get_object_text(self) -> Query[GetObjectTextInput, ObjectText]:
        return self._get_object_text

    def put_object(self) -> Mutation[PutObjectInput, StoredObject]:
        return self._put_object

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "amazon_s3.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Amazon S3 connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.amazon_s3.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Amazon S3 client is required")
    return Connection(reference, client.list_objects(), client.head_object(), client.get_object_text(), client.put_object(), client)

setattr(new_connection, '__module__', 'dex_connectors.amazon_s3.connection')


def new_connection_with_operations(
    list_objects: Query[ListObjectsInput, ListObjectsOutput],
    head_object: Query[HeadObjectInput, ObjectMetadata],
    get_object_text: Query[GetObjectTextInput, ObjectText],
    put_object: Mutation[PutObjectInput, StoredObject],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_objects, head_object, get_object_text, put_object)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.amazon_s3.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        access_key_id = material.get("access_key_id")
        if not isinstance(access_key_id, str) or not access_key_id:
            raise ValueError("Amazon S3 credential access_key_id is required")
        secret_access_key = material.get("secret_access_key")
        if not isinstance(secret_access_key, str) or not secret_access_key:
            raise ValueError("Amazon S3 credential secret_access_key is required")
        session_token = material.get("session_token")
        if session_token is None:
            session_token = ''
        if not isinstance(session_token, str):
            raise ValueError("Amazon S3 credential session_token has an invalid type")
        return Credentials(SecretString(access_key_id), SecretString(secret_access_key), SecretString(session_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Amazon S3 connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Amazon S3 project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Amazon S3 project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.amazon_s3.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"region", "endpoint", "addressingStyle", "defaultBucket", "listPageSize", "maxResponseBytes", "maxTextBytes", "maxUploadBytes"}:
        raise ValueError("Amazon S3 configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "region" in value and value["region"] != '':
        region = value["region"]
        if not (isinstance(region, str)):
            raise ValueError("Amazon S3 configuration region has an invalid type")
        config_arguments['region'] = region
    if "endpoint" in value and value["endpoint"] != '':
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Amazon S3 configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    else:
        config_arguments['endpoint'] = ''
    if "addressingStyle" in value and value["addressingStyle"] != '':
        addressing_style = value["addressingStyle"]
        if not (isinstance(addressing_style, str) and addressing_style in ('auto', 'virtualHosted', 'path')):
            raise ValueError("Amazon S3 configuration addressingStyle has an invalid type")
        config_arguments['addressing_style'] = addressing_style
    if "defaultBucket" in value and value["defaultBucket"] != '':
        default_bucket = value["defaultBucket"]
        if not (isinstance(default_bucket, str)):
            raise ValueError("Amazon S3 configuration defaultBucket has an invalid type")
        config_arguments['default_bucket'] = default_bucket
    else:
        config_arguments['default_bucket'] = ''
    if "listPageSize" in value and value["listPageSize"] != '':
        list_page_size = value["listPageSize"]
        if not (type(list_page_size) is int):
            raise ValueError("Amazon S3 configuration listPageSize has an invalid type")
        config_arguments['list_page_size'] = list_page_size
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Amazon S3 configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxTextBytes" in value and value["maxTextBytes"] != '':
        max_text_bytes = value["maxTextBytes"]
        if not (type(max_text_bytes) is int):
            raise ValueError("Amazon S3 configuration maxTextBytes has an invalid type")
        config_arguments['max_text_bytes'] = max_text_bytes
    if "maxUploadBytes" in value and value["maxUploadBytes"] != '':
        max_upload_bytes = value["maxUploadBytes"]
        if not (type(max_upload_bytes) is int):
            raise ValueError("Amazon S3 configuration maxUploadBytes has an invalid type")
        config_arguments['max_upload_bytes'] = max_upload_bytes
    return Config(**config_arguments)
