# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchWebInput supplies a public web search query.
@dataclass(frozen=True, slots=True)
class SearchWebInput:
    # Query is a nonblank search phrase of at most 256 Unicode characters.
    query: str = ''


setattr(SearchWebInput, '__module__', 'dex_connectors.bocha.models')


# WebResult contains a title, HTTPS URL, and short search excerpt.
@dataclass(frozen=True, slots=True)
class WebResult:
    # Title is the provider's page title.
    title: str = ''
    # URL is the page's public HTTPS URL.
    url: str = ''
    # Snippet is the provider's short search excerpt.
    snippet: str = ''


setattr(WebResult, '__module__', 'dex_connectors.bocha.models')


# SearchWebOutput contains up to five public web results.
@dataclass(frozen=True, slots=True)
class SearchWebOutput:
    # Results are the public pages returned in provider ranking order.
    results: tuple[WebResult, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'results', _replace_nil_slices(self.results, 1))


SearchWebOutput.__annotations__['results'] = tuple[WebResult, ...] | None
SearchWebOutput.__dataclass_fields__['results'].type = tuple[WebResult, ...] | None
setattr(SearchWebOutput, '__module__', 'dex_connectors.bocha.models')
