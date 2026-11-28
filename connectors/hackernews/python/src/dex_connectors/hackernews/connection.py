# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# connectorctl: handwritten facade
"""Hacker News connection for a named project connection."""

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query
from dex_connectors.sdk.projectconfig import LoadedProject

from .client import (
    CONNECTOR_ID,
    GET_ARTICLE_THREAD_DEFINITION,
    GET_ITEM_DEFINITION,
    GET_USER_DEFINITION,
    LIST_STORY_IDS_DEFINITION,
    PROVIDER,
    SEARCH_ITEMS_DEFINITION,
    SEARCH_STORIES_DEFINITION,
    Client,
    Config,
)
from .models import (
    ArticleThread,
    GetArticleThreadInput,
    GetItemInput,
    GetUserInput,
    Item,
    ItemSearchPage,
    ListStoryIDsInput,
    SearchItemsInput,
    SearchStoriesInput,
    StoryIDs,
    StorySearchPage,
    User,
)


class Connection:
    """Nonserializable Hacker News operation bindings for a named project connection."""

    __slots__ = (
        "_list_story_ids",
        "_get_item",
        "_get_user",
        "_get_article_thread",
        "_search_items",
        "_search_stories",
        "_client",
        "reference",
    )

    def __init__(
        self,
        reference: ConnectionRef,
        list_story_ids: Query[ListStoryIDsInput, StoryIDs],
        get_item: Query[GetItemInput, Item],
        client: Client | None = None,
        *,
        search_items: Query[SearchItemsInput, ItemSearchPage] | None = None,
        search_stories: Query[SearchStoriesInput, StorySearchPage] | None = None,
        get_user: Query[GetUserInput, User] | None = None,
        get_article_thread: Query[GetArticleThreadInput, ArticleThread] | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or list_story_ids is None or get_item is None:
            raise ValueError("Hacker News connection provider and operations are required")
        if get_article_thread is None and client is not None:
            get_article_thread = client.get_article_thread()
        if search_items is None and client is not None:
            search_items = client.search_items()
        if search_stories is None and client is not None:
            search_stories = client.search_stories()
        if get_user is None and client is not None:
            get_user = client.get_user()
        if (
            list_story_ids.definition().operation != LIST_STORY_IDS_DEFINITION.operation
            or get_item.definition().operation != GET_ITEM_DEFINITION.operation
            or (
                get_article_thread is not None
                and get_article_thread.definition().operation != GET_ARTICLE_THREAD_DEFINITION.operation
            )
            or (
                search_items is not None
                and search_items.definition().operation != SEARCH_ITEMS_DEFINITION.operation
            )
            or (
                search_stories is not None
                and search_stories.definition().operation != SEARCH_STORIES_DEFINITION.operation
            )
            or (get_user is not None and get_user.definition().operation != GET_USER_DEFINITION.operation)
        ):
            raise ValueError("Hacker News connection operation identity differs")
        self.reference = reference
        self._list_story_ids = list_story_ids
        self._get_item = get_item
        self._get_article_thread = get_article_thread
        self._search_items = search_items
        self._search_stories = search_stories
        self._get_user = get_user
        self._client = client

    def list_story_ids(self) -> Query[ListStoryIDsInput, StoryIDs]:
        """Return the feed snapshot Query used by an application Step."""
        return self._list_story_ids

    def get_item(self) -> Query[GetItemInput, Item]:
        """Return the item Query used by an application Step."""
        return self._get_item

    def get_article_thread(self) -> Query[GetArticleThreadInput, ArticleThread]:
        """Return the article Query, raising ValueError when a test connection omitted it."""
        if self._get_article_thread is None:
            raise ValueError("bind get_article_thread to register an article Step")
        return self._get_article_thread

    def search_items(self) -> Query[SearchItemsInput, ItemSearchPage]:
        """Return the Query for indexed items, raising ValueError when omitted by a test connection."""
        if self._search_items is None:
            raise ValueError("bind search_items to register this search Step")
        return self._search_items

    def search_stories(self) -> Query[SearchStoriesInput, StorySearchPage]:
        """Return the Query for indexed stories, raising ValueError when omitted by a test connection."""
        if self._search_stories is None:
            raise ValueError("bind search_stories to register this search Step")
        return self._search_stories

    def get_user(self) -> Query[GetUserInput, User]:
        """Return the public profile Query, raising ValueError for a test connection that omitted it."""
        if self._get_user is None:
            raise ValueError(
                "bind get_user when constructing this test connection to register a profile Step"
            )
        return self._get_user

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport out of logs."""
        return "hackernews.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Hacker News connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Hacker News client is required")
    return Connection(reference, client.list_story_ids(), client.get_item(), client)


def new_connection_with_operations(
    list_story_ids: Query[ListStoryIDsInput, StoryIDs],
    get_item: Query[GetItemInput, Item],
    reference: ConnectionRef,
    *,
    search_items: Query[SearchItemsInput, ItemSearchPage] | None = None,
    search_stories: Query[SearchStoriesInput, StorySearchPage] | None = None,
    get_user: Query[GetUserInput, User] | None = None,
    get_article_thread: Query[GetArticleThreadInput, ArticleThread] | None = None,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(
        reference,
        list_story_ids,
        get_item,
        search_items=search_items,
        search_stories=search_stories,
        get_user=get_user,
        get_article_thread=get_article_thread,
    )


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open the declared connection settings; the public API needs no credentials."""
    if project is None:
        raise ValueError("Hacker News connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Hacker News project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Hacker News project connection provider differs")
    return new_connection(Client(_decode_config(matching[0].configuration), http_client), reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes", "searchBaseUrl"}:
        raise ValueError("Hacker News configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    search_base_url = value.get("searchBaseUrl", "")
    if not isinstance(base_url, str) or not isinstance(search_base_url, str):
        raise ValueError("Hacker News configuration baseUrl and searchBaseUrl must be strings")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Hacker News configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes, search_base_url)
