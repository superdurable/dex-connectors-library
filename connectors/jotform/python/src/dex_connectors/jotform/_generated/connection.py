# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_SUBMISSION_DEFINITION, LIST_FORMS_DEFINITION
from ..models import FormsPage, GetSubmissionInput, ListFormsInput, SubmissionMetadata

class Connection:

    __slots__ = ('_list_forms', '_get_submission', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_forms: Query[ListFormsInput, FormsPage],
        get_submission: Query[GetSubmissionInput, SubmissionMetadata],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_forms is None or get_submission is None:
            raise ValueError("Jotform connection provider and operations are required")
        if list_forms.definition().operation != LIST_FORMS_DEFINITION.operation:
            raise ValueError("Jotform connection operation identity differs")
        if get_submission.definition().operation != GET_SUBMISSION_DEFINITION.operation:
            raise ValueError("Jotform connection operation identity differs")
        self.reference = reference
        self._list_forms = list_forms
        self._get_submission = get_submission
        self._client = client

    def list_forms(self) -> Query[ListFormsInput, FormsPage]:
        return self._list_forms

    def get_submission(self) -> Query[GetSubmissionInput, SubmissionMetadata]:
        return self._get_submission

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "jotform.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Jotform connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.jotform.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Jotform client is required")
    return Connection(reference, client.list_forms(), client.get_submission(), client)

setattr(new_connection, '__module__', 'dex_connectors.jotform.connection')


def new_connection_with_operations(
    list_forms: Query[ListFormsInput, FormsPage],
    get_submission: Query[GetSubmissionInput, SubmissionMetadata],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_forms, get_submission)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.jotform.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Jotform stored credential does not match the connector credential fields")
        if set(material) - set(('api_key',)):
            raise ValueError("Jotform stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Jotform credential api_key has an invalid type")
        if not api_key:
            raise ValueError("Jotform credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Jotform connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Jotform project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Jotform project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.jotform.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Jotform configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Jotform configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Jotform configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Jotform configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
