# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, ADD_NOTE_DEFINITION, CREATE_SHARED_DRAFT_DEFINITION, GET_CONVERSATION_DEFINITION, LIST_CONVERSATIONS_DEFINITION, LIST_INBOXES_DEFINITION, UPDATE_CONVERSATION_DEFINITION
from ..models import AddNoteInput, AddNoteOutput, ConversationDetails, CreateSharedDraftInput, GetConversationInput, ListConversationsInput, ListConversationsOutput, ListInboxesInput, ListInboxesOutput, SharedDraft, UpdateConversationInput, UpdateConversationOutput

class Connection:

    __slots__ = ('_list_inboxes', '_list_conversations', '_get_conversation', '_update_conversation', '_add_note', '_create_shared_draft', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_inboxes: Query[ListInboxesInput, ListInboxesOutput],
        list_conversations: Query[ListConversationsInput, ListConversationsOutput],
        get_conversation: Query[GetConversationInput, ConversationDetails],
        update_conversation: Mutation[UpdateConversationInput, UpdateConversationOutput],
        add_note: Mutation[AddNoteInput, AddNoteOutput],
        create_shared_draft: Mutation[CreateSharedDraftInput, SharedDraft],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_inboxes is None or list_conversations is None or get_conversation is None or update_conversation is None or add_note is None or create_shared_draft is None:
            raise ValueError("Hiver connection provider and operations are required")
        if list_inboxes.definition().operation != LIST_INBOXES_DEFINITION.operation:
            raise ValueError("Hiver connection operation identity differs")
        if list_conversations.definition().operation != LIST_CONVERSATIONS_DEFINITION.operation:
            raise ValueError("Hiver connection operation identity differs")
        if get_conversation.definition().operation != GET_CONVERSATION_DEFINITION.operation:
            raise ValueError("Hiver connection operation identity differs")
        if update_conversation.definition().operation != UPDATE_CONVERSATION_DEFINITION.operation:
            raise ValueError("Hiver connection operation identity differs")
        if add_note.definition().operation != ADD_NOTE_DEFINITION.operation:
            raise ValueError("Hiver connection operation identity differs")
        if create_shared_draft.definition().operation != CREATE_SHARED_DRAFT_DEFINITION.operation:
            raise ValueError("Hiver connection operation identity differs")
        self.reference = reference
        self._list_inboxes = list_inboxes
        self._list_conversations = list_conversations
        self._get_conversation = get_conversation
        self._update_conversation = update_conversation
        self._add_note = add_note
        self._create_shared_draft = create_shared_draft
        self._client = client

    def list_inboxes(self) -> Query[ListInboxesInput, ListInboxesOutput]:
        return self._list_inboxes

    def list_conversations(self) -> Query[ListConversationsInput, ListConversationsOutput]:
        return self._list_conversations

    def get_conversation(self) -> Query[GetConversationInput, ConversationDetails]:
        return self._get_conversation

    def update_conversation(self) -> Mutation[UpdateConversationInput, UpdateConversationOutput]:
        return self._update_conversation

    def add_note(self) -> Mutation[AddNoteInput, AddNoteOutput]:
        return self._add_note

    def create_shared_draft(self) -> Mutation[CreateSharedDraftInput, SharedDraft]:
        return self._create_shared_draft

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "hiver.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Hiver connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.hiver.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Hiver client is required")
    return Connection(reference, client.list_inboxes(), client.list_conversations(), client.get_conversation(), client.update_conversation(), client.add_note(), client.create_shared_draft(), client)

setattr(new_connection, '__module__', 'dex_connectors.hiver.connection')


def new_connection_with_operations(
    list_inboxes: Query[ListInboxesInput, ListInboxesOutput],
    list_conversations: Query[ListConversationsInput, ListConversationsOutput],
    get_conversation: Query[GetConversationInput, ConversationDetails],
    update_conversation: Mutation[UpdateConversationInput, UpdateConversationOutput],
    add_note: Mutation[AddNoteInput, AddNoteOutput],
    create_shared_draft: Mutation[CreateSharedDraftInput, SharedDraft],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_inboxes, list_conversations, get_conversation, update_conversation, add_note, create_shared_draft)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.hiver.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Hiver credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Hiver connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Hiver project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Hiver project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.hiver.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "requestIntervalMilliseconds"}:
        raise ValueError("Hiver configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Hiver configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "requestIntervalMilliseconds" in value:
        request_interval_milliseconds = value["requestIntervalMilliseconds"]
        if not (type(request_interval_milliseconds) is int):
            raise ValueError("Hiver configuration requestIntervalMilliseconds has an invalid type")
        config_arguments['request_interval_milliseconds'] = request_interval_milliseconds
    return Config(**config_arguments)
