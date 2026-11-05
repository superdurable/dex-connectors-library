# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_FORM_DEFINITION, LIST_FORMS_DEFINITION, LIST_RESPONSES_DEFINITION, UPSERT_WEBHOOK_DEFINITION
from ..models import Form, FormPage, GetFormInput, ListFormsInput, ListResponsesInput, ResponsePage, UpsertWebhookInput, Webhook

class Connection:

    __slots__ = ('_list_forms', '_get_form', '_list_responses', '_upsert_webhook', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_forms: Query[ListFormsInput, FormPage],
        get_form: Query[GetFormInput, Form],
        list_responses: Query[ListResponsesInput, ResponsePage],
        upsert_webhook: Mutation[UpsertWebhookInput, Webhook],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_forms is None or get_form is None or list_responses is None or upsert_webhook is None:
            raise ValueError("Typeform connection provider and operations are required")
        if list_forms.definition().operation != LIST_FORMS_DEFINITION.operation:
            raise ValueError("Typeform connection operation identity differs")
        if get_form.definition().operation != GET_FORM_DEFINITION.operation:
            raise ValueError("Typeform connection operation identity differs")
        if list_responses.definition().operation != LIST_RESPONSES_DEFINITION.operation:
            raise ValueError("Typeform connection operation identity differs")
        if upsert_webhook.definition().operation != UPSERT_WEBHOOK_DEFINITION.operation:
            raise ValueError("Typeform connection operation identity differs")
        self.reference = reference
        self._list_forms = list_forms
        self._get_form = get_form
        self._list_responses = list_responses
        self._upsert_webhook = upsert_webhook
        self._client = client

    def list_forms(self) -> Query[ListFormsInput, FormPage]:
        return self._list_forms

    def get_form(self) -> Query[GetFormInput, Form]:
        return self._get_form

    def list_responses(self) -> Query[ListResponsesInput, ResponsePage]:
        return self._list_responses

    def upsert_webhook(self) -> Mutation[UpsertWebhookInput, Webhook]:
        return self._upsert_webhook

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "typeform.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Typeform connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.typeform.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Typeform client is required")
    return Connection(reference, client.list_forms(), client.get_form(), client.list_responses(), client.upsert_webhook(), client)

setattr(new_connection, '__module__', 'dex_connectors.typeform.connection')


def new_connection_with_operations(
    list_forms: Query[ListFormsInput, FormPage],
    get_form: Query[GetFormInput, Form],
    list_responses: Query[ListResponsesInput, ResponsePage],
    upsert_webhook: Mutation[UpsertWebhookInput, Webhook],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_forms, get_form, list_responses, upsert_webhook)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.typeform.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        selected_method = material.get('auth_method')
        if selected_method is None:
            selected_method = ''
        if not isinstance(selected_method, str):
            raise ValueError("Typeform credential auth_method is invalid")
        method = selected_method or "personal-access-token"
        values: dict[str, Any] = {'auth_method_id': method}
        if method == "personal-access-token":
            access_token = material.get("access_token")
            if access_token is not None and not isinstance(access_token, str):
                raise ValueError("Typeform credential access_token has an invalid type")
            if not access_token:
                raise ValueError("Typeform credential access_token is required")
            if access_token:
                values["access_token"] = SecretString(access_token)
            webhook_secret = material.get("webhook_secret")
            if webhook_secret is not None and not isinstance(webhook_secret, str):
                raise ValueError("Typeform credential webhook_secret has an invalid type")
            if webhook_secret:
                values["webhook_secret"] = SecretString(webhook_secret)
        else:
            raise ValueError("Typeform credential auth_method is invalid")
        return Credentials(**values)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Typeform connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Typeform project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Typeform project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.typeform.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"dataCenter", "maxResponseBytes", "webhookMaxBodyBytes"}:
        raise ValueError("Typeform configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "dataCenter" in value:
        data_center = value["dataCenter"]
        if not (isinstance(data_center, str) and data_center in ('us', 'eu', 'newEu')):
            raise ValueError("Typeform configuration dataCenter has an invalid type")
        config_arguments['data_center'] = data_center
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Typeform configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "webhookMaxBodyBytes" in value:
        webhook_max_body_bytes = value["webhookMaxBodyBytes"]
        if not (type(webhook_max_body_bytes) is int):
            raise ValueError("Typeform configuration webhookMaxBodyBytes has an invalid type")
        config_arguments['webhook_max_body_bytes'] = webhook_max_body_bytes
    return Config(**config_arguments)
