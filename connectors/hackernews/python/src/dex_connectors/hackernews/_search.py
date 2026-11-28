# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Hacker News Search API filters, paging and indexed public content."""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from html.parser import HTMLParser
from typing import Any, Never, TypeVar
from urllib.parse import urlencode

from .models import (
    ItemSearchHit,
    ItemSearchPage,
    SearchItemsInput,
    SearchStoriesInput,
    StorySearchHit,
    StorySearchPage,
)

MAX_STORY_TEXT_CHARACTERS = 2000
_KNOWN_STORY_TAGS = ("story", "show_hn", "ask_hn", "front_page")
_ITEM_TYPES = ("story", "comment", "poll", "pollopt")
_ZERO_TIME = datetime.min.replace(tzinfo=UTC)
_EPOCH = datetime(1970, 1, 1, tzinfo=UTC)
_Hit = TypeVar("_Hit")
_WHITESPACE = re.compile(r"[\t-\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+")


@dataclass(frozen=True, slots=True)
class _SearchRequest:
    path: str
    page: int
    hits_per_page: int


def story_search_request(value: SearchStoriesInput) -> _SearchRequest:
    if not isinstance(value, SearchStoriesInput):
        raise ValueError("story search input is invalid")
    if value.sort_by not in (None, "", "relevance", "date"):
        raise ValueError("sortBy must be relevance or date")
    if not isinstance(value.query, str) or len(value.query.encode()) > 512:
        raise ValueError("query must be at most 512 bytes")
    tags = ["story"]
    for tag in value.tags or ():
        if tag not in _KNOWN_STORY_TAGS:
            raise ValueError("tags must be story, show_hn, ask_hn, or front_page")
        if tag not in tags:
            tags.append(tag)
    if type(value.min_points) is not int or value.min_points < 0:
        raise ValueError("minPoints cannot be negative")
    if (
        value.created_after != _ZERO_TIME
        and value.created_before != _ZERO_TIME
        and value.created_before <= value.created_after
    ):
        raise ValueError("createdBefore must be later than createdAfter")
    size = _page_size(value.hits_per_page)
    if type(value.page) is not int or value.page < 0:
        raise ValueError("page cannot be negative")
    if value.page * size >= 1000:
        raise ValueError(
            "page times hitsPerPage must be below 1000, the hits the Search API serves for one query"
        )
    filters = _after_filter(value.created_after)
    if value.created_before != _ZERO_TIME:
        seconds = _unix_seconds(value.created_before) + int(value.created_before.microsecond != 0)
        filters.append(f"created_at_i<{seconds}")
    if value.min_points:
        filters.append(f"points>={value.min_points}")
    return _request(value.query, value.sort_by, ",".join(tags), filters, value.page, size)


def item_search_request(value: SearchItemsInput) -> _SearchRequest:
    if (
        not isinstance(value, SearchItemsInput)
        or not isinstance(value.query, str)
        or len(value.query.encode()) > 512
        or type(value.page) is not int
        or value.page < 0
    ):
        raise ValueError("Hacker News item search query or page is invalid")
    size = _page_size(value.hits_per_page)
    if value.page * size >= 1000:
        raise ValueError("Hacker News item search page size is invalid")
    if value.type and value.type not in _ITEM_TYPES:
        raise ValueError("Hacker News item type is invalid")
    if value.sort_by not in (None, "", "relevance", "date"):
        raise ValueError("Hacker News item search sort is invalid")
    return _request(
        value.query, value.sort_by, value.type, _after_filter(value.created_after), value.page, size
    )


def _page_size(value: int) -> int:
    if type(value) is not int or not 0 <= value <= 100:
        raise ValueError("hitsPerPage must be from 1 through 100")
    return value or 20


def _unix_seconds(value: datetime) -> int:
    difference = value - _EPOCH
    return difference.days * 86400 + difference.seconds


def _after_filter(after: datetime) -> list[str]:
    return [] if after == _ZERO_TIME else [f"created_at_i>{_unix_seconds(after)}"]


def _request(
    query: str, sort: str | None, tag_filter: str, filters: list[str], page: int, size: int
) -> _SearchRequest:
    parameters = {"page": str(page), "hitsPerPage": str(size)}
    if tag_filter:
        parameters["tags"] = tag_filter
    if query:
        parameters["query"] = query
    if filters:
        parameters["numericFilters"] = ",".join(filters)
    path = "/search_by_date" if sort == "date" else "/search"
    return _SearchRequest(path + "?" + urlencode(sorted(parameters.items())), page, size)


def story_search_page(body: bytes, request: _SearchRequest) -> StorySearchPage:
    hits, pages, total = _read_page(body, request, _story_hit)
    return StorySearchPage(
        stories=hits,
        page=request.page,
        pages=pages,
        total_hits=total,
        hits_per_page=request.hits_per_page,
        next_page=request.page + 1 if request.page + 1 < pages else 0,
    )


def item_search_page(body: bytes, request: _SearchRequest, item_type: str) -> ItemSearchPage:
    items, pages, total = _read_page(body, request, lambda hit: _item_hit(hit, item_type))
    return ItemSearchPage(
        items=items,
        page=request.page,
        pages=pages,
        total_hits=total,
        hits_per_page=request.hits_per_page,
        next_page=request.page + 1 if request.page + 1 < pages else 0,
    )


def _reject_number(value: str) -> Never:
    raise ValueError("non-JSON number")


def _read_page(
    body: bytes, request: _SearchRequest, read_hit: Callable[[dict[str, Any]], _Hit]
) -> tuple[tuple[_Hit, ...], int, int]:
    document = _fields(json.loads(body, parse_constant=_reject_number))
    rows = document.get("hits")
    if not isinstance(rows, list) or any(
        document.get(name) is None for name in ("page", "nbpages", "nbhits")
    ):
        raise ValueError("invalid search page")
    page, pages, total = (_integer(document, key) for key in ("page", "nbpages", "nbhits"))
    if page != request.page or pages < 0 or total < 0 or len(rows) > request.hits_per_page:
        raise ValueError("inconsistent search page")
    return tuple(read_hit(_fields(row)) for row in rows), pages, total


def _fields(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("expected a search record")
    return {key.lower(): field for key, field in value.items() if field is not None}


def _integer(document: dict[str, Any], name: str) -> int:
    value = document.get(name, 0)
    if type(value) is not int or not -(2**63) <= value < 2**63:
        raise ValueError("invalid search hit number")
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name, "")
    if not isinstance(value, str):
        raise ValueError("invalid search hit text")
    return value


def _hit_id(document: dict[str, Any]) -> int:
    value = _string(document, "objectid")
    if not re.fullmatch(r"[+-]?[0-9]+", value):
        raise ValueError("invalid search hit ID")
    item_id = int(value)
    if not 0 < item_id < 2**63:
        raise ValueError("invalid search hit ID")
    return item_id


def _created(document: dict[str, Any]) -> datetime:
    seconds = _integer(document, "created_at_i")
    if not 0 < seconds <= 253402300799:
        raise ValueError("invalid search hit time")
    return datetime.fromtimestamp(seconds, UTC)


def _tags(document: dict[str, Any]) -> list[str]:
    tags = document.get("_tags", [])
    if not isinstance(tags, list) or any(tag is not None and not isinstance(tag, str) for tag in tags):
        raise ValueError("invalid search hit tags")
    return [tag for tag in tags if tag is not None]


def _story_hit(document: dict[str, Any]) -> StorySearchHit:
    item_id = _hit_id(document)
    tags = _tags(document)
    text = _plain_text(_string(document, "story_text"))
    return StorySearchHit(
        id=item_id,
        title=_string(document, "title"),
        url=_string(document, "url"),
        author=_string(document, "author"),
        discussion_url=f"https://news.ycombinator.com/item?id={item_id}",
        created_at=_created(document),
        points=_integer(document, "points"),
        comment_count=_integer(document, "num_comments"),
        tags=tuple(tag for tag in _KNOWN_STORY_TAGS if tag in tags),
        story_text=text[:MAX_STORY_TEXT_CHARACTERS],
        is_story_text_truncated=len(text) > MAX_STORY_TEXT_CHARACTERS,
    )


def _item_hit(document: dict[str, Any], requested_type: str) -> ItemSearchHit:
    item_id = _hit_id(document)
    kind = next((tag for tag in _tags(document) if tag in _ITEM_TYPES), "")
    if not kind or (requested_type and kind != requested_type):
        raise ValueError("unexpected item type")
    title, story_title = _string(document, "title"), _string(document, "story_title")
    comment, story = _string(document, "comment_text"), _string(document, "story_text")
    return ItemSearchHit(
        id=item_id,
        type=kind,
        title=title or story_title,
        url=_string(document, "url"),
        author=_string(document, "author"),
        discussion_url=f"https://news.ycombinator.com/item?id={item_id}",
        created_at=_created(document),
        text=_plain_text(comment if kind == "comment" else story)[:MAX_STORY_TEXT_CHARACTERS],
        parent_id=_integer(document, "parent_id"),
        story_id=_integer(document, "story_id"),
    )


class _StoryTextParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.fragments: list[str] = []
        self.paragraphs: list[str] = []

    def handle_data(self, data: str) -> None:
        self.fragments.append(data)

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        self.handle_endtag(tag)

    def handle_endtag(self, tag: str) -> None:
        if tag in ("p", "pre", "br", "div", "li", "blockquote"):
            self.end_paragraph()

    def end_paragraph(self) -> None:
        text = _WHITESPACE.sub(" ", "".join(self.fragments)).strip(" ")
        if text:
            self.paragraphs.append(text)
        self.fragments.clear()


def _plain_text(html: str) -> str:
    parser = _StoryTextParser()
    parser.feed(html)
    parser.close()
    parser.end_paragraph()
    return "\n\n".join(parser.paragraphs)
