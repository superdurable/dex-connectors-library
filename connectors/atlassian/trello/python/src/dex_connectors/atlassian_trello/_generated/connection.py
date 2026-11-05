# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, ADD_COMMENT_DEFINITION, CREATE_CARD_DEFINITION, GET_CARD_DEFINITION, LIST_CARDS_DEFINITION, UPDATE_CARD_DEFINITION
from ..models import AddCommentInput, AddCommentOutput, Card, CreateCardInput, CreateCardOutput, GetCardInput, ListCardsInput, ListCardsOutput, UpdateCardInput, UpdateCardOutput

class Connection:

    __slots__ = ('_list_cards', '_get_card', '_create_card', '_update_card', '_add_comment', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_cards: Query[ListCardsInput, ListCardsOutput],
        get_card: Query[GetCardInput, Card],
        create_card: Mutation[CreateCardInput, CreateCardOutput],
        update_card: Mutation[UpdateCardInput, UpdateCardOutput],
        add_comment: Mutation[AddCommentInput, AddCommentOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_cards is None or get_card is None or create_card is None or update_card is None or add_comment is None:
            raise ValueError("Trello connection provider and operations are required")
        if list_cards.definition().operation != LIST_CARDS_DEFINITION.operation:
            raise ValueError("Trello connection operation identity differs")
        if get_card.definition().operation != GET_CARD_DEFINITION.operation:
            raise ValueError("Trello connection operation identity differs")
        if create_card.definition().operation != CREATE_CARD_DEFINITION.operation:
            raise ValueError("Trello connection operation identity differs")
        if update_card.definition().operation != UPDATE_CARD_DEFINITION.operation:
            raise ValueError("Trello connection operation identity differs")
        if add_comment.definition().operation != ADD_COMMENT_DEFINITION.operation:
            raise ValueError("Trello connection operation identity differs")
        self.reference = reference
        self._list_cards = list_cards
        self._get_card = get_card
        self._create_card = create_card
        self._update_card = update_card
        self._add_comment = add_comment
        self._client = client

    def list_cards(self) -> Query[ListCardsInput, ListCardsOutput]:
        return self._list_cards

    def get_card(self) -> Query[GetCardInput, Card]:
        return self._get_card

    def create_card(self) -> Mutation[CreateCardInput, CreateCardOutput]:
        return self._create_card

    def update_card(self) -> Mutation[UpdateCardInput, UpdateCardOutput]:
        return self._update_card

    def add_comment(self) -> Mutation[AddCommentInput, AddCommentOutput]:
        return self._add_comment

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "atlassian_trello.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Trello connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.atlassian_trello.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Trello client is required")
    return Connection(reference, client.list_cards(), client.get_card(), client.create_card(), client.update_card(), client.add_comment(), client)

setattr(new_connection, '__module__', 'dex_connectors.atlassian_trello.connection')


def new_connection_with_operations(
    list_cards: Query[ListCardsInput, ListCardsOutput],
    get_card: Query[GetCardInput, Card],
    create_card: Mutation[CreateCardInput, CreateCardOutput],
    update_card: Mutation[UpdateCardInput, UpdateCardOutput],
    add_comment: Mutation[AddCommentInput, AddCommentOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_cards, get_card, create_card, update_card, add_comment)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.atlassian_trello.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Trello stored credential does not match the connector credential fields")
        if set(material) - set(('api_key', 'token')):
            raise ValueError("Trello stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Trello credential api_key has an invalid type")
        token = material.get("token")
        if token is not None and not (isinstance(token, str)):
            raise ValueError("Trello credential token has an invalid type")
        if not api_key:
            raise ValueError("Trello credential api_key is required")
        if not token:
            raise ValueError("Trello credential token is required")
        return Credentials(SecretString(api_key), SecretString(token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Trello connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Trello project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Trello project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.atlassian_trello.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes"}:
        raise ValueError("Trello configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Trello configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Trello configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
