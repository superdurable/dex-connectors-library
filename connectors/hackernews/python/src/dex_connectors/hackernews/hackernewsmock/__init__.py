# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# connectorctl: handwritten facade
"""Manifest-backed Hacker News scripts for consuming Flow tests."""

from datetime import UTC, datetime
from typing import TypeVar

from dex_connectors.sdk import Call, ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    FAILURE_PROVIDER,
    FOUND,
    GET_ARTICLE_THREAD_DEFINITION,
    GET_ITEM_DEFINITION,
    GET_USER_DEFINITION,
    LIST_STORY_IDS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER,
    PROVIDER_REJECTED,
    SEARCH_ITEMS_DEFINITION,
    SEARCH_STORIES_DEFINITION,
)
from ..connection import Connection, new_connection_with_operations
from ..models import (
    ArticleComment,
    ArticleThread,
    GetArticleThreadInput,
    GetItemInput,
    GetUserInput,
    Item,
    ItemSearchHit,
    ItemSearchPage,
    ListStoryIDsInput,
    SearchItemsInput,
    SearchStoriesInput,
    StoryIDs,
    StorySearchHit,
    StorySearchPage,
    User,
)


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


def get_user_found(value: User, failure: Failure | None = None) -> Case[User]:
    """Script the successful profile route."""
    return Case.branch_result(FOUND, value, failure)


def get_user_public_user() -> Case[User]:
    """Return the manifest's publicUser profile."""
    return get_user_found(
        User(
            id="langtools",
            created=1700000000,
            karma=100,
            submitted=(42003,),
            profile_url="https://news.ycombinator.com/user?id=langtools",
        )
    )


def get_article_thread_found(value: ArticleThread, failure: Failure | None = None) -> Case[ArticleThread]:
    """Script the article found route."""
    return Case.branch_result(FOUND, value, failure)


def get_article_thread_compiler_thread() -> Case[ArticleThread]:
    """Return the manifest's compilerThread answer."""
    return get_article_thread_found(
        ArticleThread(
            id=42003,
            title="Show HN: A compiler explorer for small languages",
            author="langtools",
            discussion_url="https://news.ycombinator.com/item?id=42003",
            comments=(
                ArticleComment(
                    id=42004,
                    author="reviewer",
                    text_html="Useful explorer.",
                    discussion_url="https://news.ycombinator.com/item?id=42004",
                ),
            ),
        )
    )


_PUBLIC_ITEMS_PAGES = (
    ItemSearchPage(
        items=(
            ItemSearchHit(
                id=42001,
                type="story",
                title="A compiler release",
                author="compilerdev",
                created_at=datetime.fromisoformat("2026-10-26T11:00:00Z"),
                discussion_url="https://news.ycombinator.com/item?id=42001",
            ),
        ),
        page=0,
        pages=3,
        total_hits=3,
        hits_per_page=1,
        next_page=1,
    ),
    ItemSearchPage(
        items=(
            ItemSearchHit(
                id=42002,
                type="comment",
                text="A useful compiler note",
                author="reviewer",
                created_at=datetime.fromisoformat("2026-10-26T11:01:00Z"),
                discussion_url="https://news.ycombinator.com/item?id=42002",
            ),
        ),
        page=1,
        pages=3,
        total_hits=3,
        hits_per_page=1,
        next_page=2,
    ),
    ItemSearchPage(
        items=(
            ItemSearchHit(
                id=42003,
                type="poll",
                title="Compiler poll",
                author="compilerdev",
                created_at=datetime.fromisoformat("2026-10-26T11:02:00Z"),
                discussion_url="https://news.ycombinator.com/item?id=42003",
            ),
        ),
        page=2,
        pages=3,
        total_hits=3,
        hits_per_page=1,
        next_page=0,
    ),
)


def search_items_public_items(page: int = 0) -> Case[ItemSearchPage]:
    """Return a zero-based page from the manifest's publicItems answer."""
    if not 0 <= page < len(_PUBLIC_ITEMS_PAGES):
        raise ValueError("page outside the manifest mock")
    return Case.branch_result("searched", _PUBLIC_ITEMS_PAGES[page])


_COMPILER_STORIES_PAGES = (
    StorySearchPage(
        stories=(
            StorySearchHit(
                id=42003,
                title="Show HN: A compiler explorer for small languages",
                url="https://example.com/explorer",
                discussion_url="https://news.ycombinator.com/item?id=42003",
                author="langtools",
                points=12,
                comment_count=3,
                created_at=datetime.fromisoformat("2026-10-26T11:50:00Z"),
                tags=("story", "show_hn"),
            ),
            StorySearchHit(
                id=42001,
                title="A compiler release",
                url="https://example.com/compiler",
                discussion_url="https://news.ycombinator.com/item?id=42001",
                author="compilerdev",
                points=100,
                comment_count=40,
                created_at=datetime.fromisoformat("2026-10-26T11:00:00Z"),
                tags=("story", "front_page"),
            ),
        ),
        page=0,
        pages=3,
        total_hits=5,
        hits_per_page=2,
        next_page=1,
    ),
    StorySearchPage(
        stories=(
            StorySearchHit(
                id=42001,
                title="A compiler release",
                url="https://example.com/compiler",
                discussion_url="https://news.ycombinator.com/item?id=42001",
                author="compilerdev",
                points=100,
                comment_count=40,
                created_at=datetime.fromisoformat("2026-10-26T11:00:00Z"),
                tags=("story", "front_page"),
            ),
            StorySearchHit(
                id=41990,
                title="Ask HN: How do you test a compiler?",
                discussion_url="https://news.ycombinator.com/item?id=41990",
                author="testwriter",
                points=8,
                comment_count=5,
                created_at=datetime.fromisoformat("2026-10-26T09:30:00Z"),
                tags=("story", "ask_hn"),
                story_text="We fuzz the parser.\n\nWhat else catches miscompilations early?",
            ),
        ),
        page=1,
        pages=3,
        total_hits=5,
        hits_per_page=2,
        next_page=2,
    ),
    StorySearchPage(
        stories=(
            StorySearchHit(
                id=41980,
                title="Notes on compiler passes",
                url="https://example.com/passes",
                discussion_url="https://news.ycombinator.com/item?id=41980",
                author="notetaker",
                points=1,
                comment_count=0,
                created_at=datetime.fromisoformat("2026-10-26T08:15:00Z"),
                tags=("story",),
            ),
        ),
        page=2,
        pages=3,
        total_hits=5,
        hits_per_page=2,
        next_page=0,
    ),
)


def search_stories_compiler_stories(page: int = 0) -> Case[StorySearchPage]:
    """Return a zero-based page from the manifest's compilerStories answer."""
    if not 0 <= page < len(_COMPILER_STORIES_PAGES):
        raise ValueError("page outside the manifest mock")
    return Case.branch_result("searched", _COMPILER_STORIES_PAGES[page])


_SearchInput = TypeVar("_SearchInput", SearchItemsInput, SearchStoriesInput)
_SearchPage = TypeVar("_SearchPage", ItemSearchPage, StorySearchPage)


def search_stories_search_rejected() -> Case[StorySearchPage]:
    """Return the manifest's searchRejected answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        StorySearchPage(),
        Failure(
            FailureKind.PROVIDER_REJECTION,
            PROVIDER,
            "searchStories",
            "Hacker News Search rejected the story search.",
        ),
    )


class _SearchQuery(Query[_SearchInput, _SearchPage]):
    def _answer(self, call: Call, input: _SearchInput, idempotency_key: str = "") -> Case[_SearchPage]:
        if call.context is None:
            raise ValueError("connector mock requires a live Dex Step context")
        # The SDK selects a one-based mock page; the Hacker News API numbers pages from zero.
        return self._scripted.answer(
            call.context.flow_id, call.id, call.connection, input, idempotency_key, input.page + 1
        )


class Mock:
    """Script Hacker News Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "hackernews") -> None:
        self.list_story_ids_script: Query[ListStoryIDsInput, StoryIDs] = Query(LIST_STORY_IDS_DEFINITION)
        self.list_story_ids_script.default(list_story_ids_developer_feed())
        self.get_item_script: Query[GetItemInput, Item] = Query(GET_ITEM_DEFINITION)
        self.get_item_script.default(get_item_compiler_story())
        self.get_user_script: Query[GetUserInput, User] = Query(GET_USER_DEFINITION)
        self.get_user_script.default(get_user_public_user())
        self.get_article_thread_script: Query[GetArticleThreadInput, ArticleThread] = Query(
            GET_ARTICLE_THREAD_DEFINITION
        )
        self.get_article_thread_script.default(get_article_thread_compiler_thread())
        self.search_items_script: Query[SearchItemsInput, ItemSearchPage] = _SearchQuery(
            SEARCH_ITEMS_DEFINITION
        )
        self.search_items_script.default(Case.paged("searched", _PUBLIC_ITEMS_PAGES))
        self.search_stories_script: Query[SearchStoriesInput, StorySearchPage] = _SearchQuery(
            SEARCH_STORIES_DEFINITION
        )
        self.search_stories_script.default(Case.paged("searched", _COMPILER_STORIES_PAGES))
        self._connection = new_connection_with_operations(
            self.list_story_ids_script,
            self.get_item_script,
            ConnectionRef(PROVIDER, connection_name),
            search_items=self.search_items_script,
            search_stories=self.search_stories_script,
            get_user=self.get_user_script,
            get_article_thread=self.get_article_thread_script,
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "search_items_public_items",
    "search_stories_compiler_stories",
    "search_stories_search_rejected",
    "Mock",
    "get_article_thread_compiler_thread",
    "get_article_thread_found",
    "get_user_found",
    "get_user_public_user",
    "list_story_ids_listed",
    "list_story_ids_developer_feed",
    "list_story_ids_feed_rejected",
    "get_item_found",
    "get_item_compiler_story",
    "get_item_missing",
    "get_item_rejected",
]
