# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from .timestamps import parse_go_datetime, validate_go_datetime

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListStoryIDsInput selects one feed snapshot. It does not select a historical time window.
@dataclass(frozen=True, slots=True)
class ListStoryIDsInput:
    # Feed is top, best, new, ask, or show. Empty selects top.
    feed: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Limit bounds returned IDs from 1 through 500. Zero selects 50.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListStoryIDsInput, '__module__', 'dex_connectors.hackernews.models')


# StoryIDs contains the start of a feed in the provider's current display order.
@dataclass(frozen=True, slots=True)
class StoryIDs:
    # IDs contains at most the requested limit. Top stories can include jobs.
    ids: tuple[int, ...] = ()
    # HasMore reports that the API returned more IDs than this operation retained. The API has no page cursor.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    # ObservedAt is the UTC time when this response was read.
    observed_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'observedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.observed_at, str):
            object.__setattr__(self, 'observed_at', parse_go_datetime(self.observed_at, 'observed_at'))
        elif self.observed_at is not None:
            validate_go_datetime(self.observed_at, 'observed_at')
        object.__setattr__(self, 'ids', _replace_nil_slices(self.ids, 1))


StoryIDs.__annotations__['ids'] = tuple[int, ...] | None
StoryIDs.__dataclass_fields__['ids'].type = tuple[int, ...] | None
setattr(StoryIDs, '__module__', 'dex_connectors.hackernews.models')


# GetItemInput identifies one story, comment, job, poll, or poll option.
@dataclass(frozen=True, slots=True)
class GetItemInput:
    # ID is a positive Hacker News item ID.
    id: int = 0


setattr(GetItemInput, '__module__', 'dex_connectors.hackernews.models')


# Item preserves public API fields and adds the exact Hacker News permalink.
# TextHTML and Title are untrusted provider text. Applications must escape or sanitize them before HTML rendering.
@dataclass(frozen=True, slots=True)
class Item:
    # ID identifies this item.
    id: int = 0
    # Type is story, comment, job, poll, or pollopt.
    type: str = ''
    # By is the author's public username, when available.
    by: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Time is the creation time in Unix seconds, when available.
    time: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Title is the provider's title, which can contain HTML entities.
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # TextHTML is the submitted story or comment body. Linked article content is not included.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URL is the submitted external URL. Ask HN and other text posts may have no URL.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DiscussionURL links directly to this story or comment on Hacker News.
    discussion_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'discussionUrl'})
    # Score is the story score. Hacker News does not expose comment scores.
    score: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Descendants is the provider's comment count for a story or poll.
    descendants: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Parent identifies a comment's parent item or a poll option's poll.
    parent: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Kids contains child IDs in the provider's ranked display order.
    kids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Deleted reports a deleted item. GetItem selects notFound instead of returning a tombstone.
    deleted: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    # Dead reports a moderated item. GetItem selects notFound instead of returning it.
    dead: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(Item, '__module__', 'dex_connectors.hackernews.models')


# GetUserInput identifies a public Hacker News user by case-sensitive username.
@dataclass(frozen=True, slots=True)
class GetUserInput:
    # ID is the username shown on the Hacker News profile page.
    id: str = ''


setattr(GetUserInput, '__module__', 'dex_connectors.hackernews.models')


# User contains public profile fields and submitted item IDs.
@dataclass(frozen=True, slots=True)
class User:
    # ID is the case-sensitive public username.
    id: str = ''
    # Created is the account creation time in Unix seconds.
    created: int = 0
    # Karma is the public score reported by Hacker News.
    karma: int = 0
    # AboutHTML is untrusted profile HTML and needs sanitization before rendering.
    about: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Submitted contains public story, poll, and comment IDs.
    submitted: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ProfileURL links to the public Hacker News profile.
    profile_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'profileUrl'})


setattr(User, '__module__', 'dex_connectors.hackernews.models')


# GetArticleThreadInput identifies a public story or poll by item ID.
@dataclass(frozen=True, slots=True)
class GetArticleThreadInput:
    # ID is a positive Hacker News article ID.
    id: int = 0


setattr(GetArticleThreadInput, '__module__', 'dex_connectors.hackernews.models')


# ArticleComment is a public comment with its replies.
@dataclass(frozen=True, slots=True)
class ArticleComment:
    # ID is the comment item ID.
    id: int = 0
    # Author is the commenter's public username, when available.
    author: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # TextHTML is untrusted comment HTML.
    text_html: str = _dataclass_field(default='', metadata={'dex_json_name': 'textHtml', 'dex_json_omit_default': True})
    # DiscussionURL links directly to the comment on Hacker News.
    discussion_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'discussionUrl'})
    # Replies contains nested public comments in provider order.
    replies: tuple["ArticleComment", ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(ArticleComment, '__module__', 'dex_connectors.hackernews.models')


# ArticleThread contains one indexed article and its public comment tree.
# Title, TextHTML, and comment text are untrusted provider content.
@dataclass(frozen=True, slots=True)
class ArticleThread:
    # ID is the article item ID.
    id: int = 0
    # Title is the article title.
    title: str = ''
    # Author is the article submitter's public username.
    author: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URL is the external source URL, when present.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # TextHTML is the submitted story body, when present.
    text_html: str = _dataclass_field(default='', metadata={'dex_json_name': 'textHtml', 'dex_json_omit_default': True})
    # DiscussionURL links to the article on Hacker News.
    discussion_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'discussionUrl'})
    # Comments are the indexed public comments in their nested order.
    comments: tuple[ArticleComment, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'comments', _replace_nil_slices(self.comments, 1))


ArticleThread.__annotations__['comments'] = tuple[ArticleComment, ...] | None
ArticleThread.__dataclass_fields__['comments'].type = tuple[ArticleComment, ...] | None
setattr(ArticleThread, '__module__', 'dex_connectors.hackernews.models')


# SearchStoriesInput selects one page of a keyword story search. Every field is optional.
@dataclass(frozen=True, slots=True)
class SearchStoriesInput:
    # Query is the full-text keyword query, at most 512 bytes, matched against story titles, URLs, authors,
    # and submitted text. Quoted phrases and -excluded words follow the Search API syntax. Empty matches
    # every story that the other filters allow.
    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SortBy is relevance or date. Empty selects relevance; date returns the newest stories first.
    sort_by: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sortBy', 'dex_json_omit_default': True})
    # Tags lists story, show_hn, ask_hn, or front_page. A story must carry every listed tag. The story tag
    # is always applied, so empty searches all stories, including Ask HN and Show HN posts.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # CreatedAfter keeps stories submitted strictly after this time. Zero sets no lower bound.
    created_after: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAfter', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # CreatedBefore keeps stories submitted strictly before this time. Zero sets no upper bound. When both
    # bounds are set, CreatedBefore must be later than CreatedAfter.
    created_before: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdBefore', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # MinPoints keeps stories with at least this many points. Zero sets no minimum; negative is invalid.
    min_points: int = _dataclass_field(default=0, metadata={'dex_json_name': 'minPoints', 'dex_json_omit_default': True})
    # Page is the zero-based result page. Zero selects the first page. Page times HitsPerPage must stay
    # below 1000, because the Search API serves only the first 1000 hits of a query.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # HitsPerPage is the page size from 1 through 100. Zero selects 20.
    hits_per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'hitsPerPage', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_after, str):
            object.__setattr__(self, 'created_after', parse_go_datetime(self.created_after, 'created_after'))
        elif self.created_after is not None:
            validate_go_datetime(self.created_after, 'created_after')
        if isinstance(self.created_before, str):
            object.__setattr__(self, 'created_before', parse_go_datetime(self.created_before, 'created_before'))
        elif self.created_before is not None:
            validate_go_datetime(self.created_before, 'created_before')


setattr(SearchStoriesInput, '__module__', 'dex_connectors.hackernews.models')


# StorySearchHit is one matching story as the Search API indexed it. Points and CommentCount can lag the
# live site. Title, Author, and StoryText are untrusted provider text; escape them before HTML rendering.
@dataclass(frozen=True, slots=True)
class StorySearchHit:
    # ID is the positive Hacker News item ID, which GetItem reads.
    id: int = 0
    # Title is the story title.
    title: str = ''
    # URL is the submitted external URL. It is empty for Ask HN and other text posts.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DiscussionURL links directly to the story's Hacker News discussion.
    discussion_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'discussionUrl'})
    # Author is the submitter's public username.
    author: str = ''
    # Points is the story score.
    points: int = 0
    # CommentCount is the story's comment count.
    comment_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'commentCount'})
    # CreatedAt is the UTC submission time, at second precision.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # Tags lists the story, show_hn, ask_hn, and front_page tags that the story carries, in that order.
    tags: tuple[str, ...] = ()
    # StoryText is the submitted text as plain text, at most MaxStoryTextCharacters code points. Paragraphs
    # are separated by a blank line, and markup is removed. It is empty when the story has no text.
    story_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'storyText', 'dex_json_omit_default': True})
    # IsStoryTextTruncated reports that StoryText was cut at MaxStoryTextCharacters.
    is_story_text_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isStoryTextTruncated', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        object.__setattr__(self, 'tags', _replace_nil_slices(self.tags, 1))


StorySearchHit.__annotations__['tags'] = tuple[str, ...] | None
StorySearchHit.__dataclass_fields__['tags'].type = tuple[str, ...] | None
setattr(StorySearchHit, '__module__', 'dex_connectors.hackernews.models')


# StorySearchPage is one page of matching stories in the requested order.
@dataclass(frozen=True, slots=True)
class StorySearchPage:
    # Stories contains at most HitsPerPage stories. It is empty when nothing matches. Successive pages of a
    # newest-first search can repeat a story when newer stories arrive between reads.
    stories: tuple[StorySearchHit, ...] = ()
    # Page is the zero-based number of this page.
    page: int = 0
    # Pages counts the readable pages. It stops at the first 1000 hits, so it can cover fewer than TotalHits.
    pages: int = 0
    # TotalHits is the Search API's count of matching stories. It can be an estimate for large result sets.
    total_hits: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalHits'})
    # HitsPerPage is the page size that this read used.
    hits_per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'hitsPerPage'})
    # NextPage is the zero-based number of the next readable page, or zero when this is the last one.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'stories', _replace_nil_slices(self.stories, 1))


StorySearchPage.__annotations__['stories'] = tuple[StorySearchHit, ...] | None
StorySearchPage.__dataclass_fields__['stories'].type = tuple[StorySearchHit, ...] | None
setattr(StorySearchPage, '__module__', 'dex_connectors.hackernews.models')


# SearchItemsInput selects one page of public Hacker News items.
@dataclass(frozen=True, slots=True)
class SearchItemsInput:
    # Query is the full-text query, at most 512 bytes; blank matches all items of the selected type.
    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Type is story, comment, poll, or pollopt; blank searches all indexed types.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SortBy is relevance or date; blank selects relevance.
    sort_by: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sortBy', 'dex_json_omit_default': True})
    # Page is zero-based; Page times HitsPerPage must stay below 1000.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # HitsPerPage is 1 through 100; zero selects 20.
    hits_per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'hitsPerPage', 'dex_json_omit_default': True})
    # CreatedAfter keeps items created strictly after this UTC time; zero sets no lower bound.
    created_after: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAfter', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_after, str):
            object.__setattr__(self, 'created_after', parse_go_datetime(self.created_after, 'created_after'))
        elif self.created_after is not None:
            validate_go_datetime(self.created_after, 'created_after')


setattr(SearchItemsInput, '__module__', 'dex_connectors.hackernews.models')


# ItemSearchHit is public indexed metadata; Title and Text are untrusted provider content.
@dataclass(frozen=True, slots=True)
class ItemSearchHit:
    # ID is the positive Hacker News item ID.
    id: int = 0
    # Type is story, comment, poll, or pollopt.
    type: str = ''
    # Title is the story or poll title, when present.
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Text is plain submitted or comment text, bounded to 2000 characters.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URL is the story's external source URL, when present.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Author is the public username, when present.
    author: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # CreatedAt is the item creation time in UTC.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # ParentID is a comment's direct parent item ID, when indexed.
    parent_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'parentId', 'dex_json_omit_default': True})
    # StoryID is the associated story ID for a comment, when indexed.
    story_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'storyId', 'dex_json_omit_default': True})
    # DiscussionURL links directly to this item.
    discussion_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'discussionUrl'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')


setattr(ItemSearchHit, '__module__', 'dex_connectors.hackernews.models')


# ItemSearchPage contains one page of public search results.
@dataclass(frozen=True, slots=True)
class ItemSearchPage:
    # Items appear in the Search API's requested order.
    items: tuple[ItemSearchHit, ...] = ()
    # Page is this zero-based page number.
    page: int = 0
    # Pages is the number of readable pages.
    pages: int = 0
    # TotalHits is the Search API's approximate total result count.
    total_hits: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalHits'})
    # HitsPerPage is the requested page size.
    hits_per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'hitsPerPage'})
    # NextPage is the next page number, or zero after the last page.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'items', _replace_nil_slices(self.items, 1))


ItemSearchPage.__annotations__['items'] = tuple[ItemSearchHit, ...] | None
ItemSearchPage.__dataclass_fields__['items'].type = tuple[ItemSearchHit, ...] | None
setattr(ItemSearchPage, '__module__', 'dex_connectors.hackernews.models')
