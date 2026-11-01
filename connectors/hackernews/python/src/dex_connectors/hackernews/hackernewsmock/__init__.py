# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# connectorctl: handwritten facade
"""Manifest-backed Hacker News scripts for consuming Flow tests."""

from datetime import UTC, datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FAILURE_PROVIDER,
    FOUND,
    GET_ITEM_DEFINITION,
    LIST_STORY_IDS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import GetItemInput, Item, ListStoryIDsInput, StoryIDs


def list_story_ids_listed(value: StoryIDs, failure: Failure | None = None) -> Case[StoryIDs]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_story_ids_developer_feed() -> Case[StoryIDs]:
    """Return the manifest's developerFeed answer."""
    return list_story_ids_listed(StoryIDs((42001,), True, datetime(2026, 10, 26, 12, tzinfo=UTC)))


def list_story_ids_feed_rejected() -> Case[StoryIDs]:
    """Return the manifest's feedRejected answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        StoryIDs(),
        Failure(
            FailureKind.PROVIDER_REJECTION,
            FAILURE_PROVIDER,
            "listStoryIDs",
            "Hacker News rejected the feed request.",
        ),
    )


def get_item_found(value: Item, failure: Failure | None = None) -> Case[Item]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_item_compiler_story() -> Case[Item]:
    """Return the manifest's compilerStory answer."""
    return get_item_found(
        Item(
            id=42001,
            type="story",
            discussion_url="https://news.ycombinator.com/item?id=42001",
            by="compilerdev",
            time=1793016000,
            title="A compiler release",
            url="https://example.com/compiler",
            score=100,
        )
    )


def get_item_missing() -> Case[Item]:
    """Return the manifest's itemMissing answer."""
    return Case.branch_result(
        NOT_FOUND,
        Item(),
        Failure(FailureKind.NOT_FOUND, FAILURE_PROVIDER, "getItem", "Hacker News item is deleted or dead."),
    )


def get_item_rejected() -> Case[Item]:
    """Return the manifest's itemRejected answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        Item(),
        Failure(
            FailureKind.PROVIDER_REJECTION,
            FAILURE_PROVIDER,
            "getItem",
            "Hacker News rejected the item request.",
        ),
    )


class Mock:
    """Script Hacker News Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "hackernews") -> None:
        self.list_story_ids_script: Query[ListStoryIDsInput, StoryIDs] = Query(LIST_STORY_IDS_DEFINITION)
        self.list_story_ids_script.default(list_story_ids_developer_feed())
        self.get_item_script: Query[GetItemInput, Item] = Query(GET_ITEM_DEFINITION)
        self.get_item_script.default(get_item_compiler_story())
        self._connection = new_connection_with_operations(
            self.list_story_ids_script, self.get_item_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_story_ids_listed",
    "list_story_ids_developer_feed",
    "list_story_ids_feed_rejected",
    "get_item_found",
    "get_item_compiler_story",
    "get_item_missing",
    "get_item_rejected",
]
