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


# ListApplicationTagsInput has no parameters; the connection identifies the app.
@dataclass(frozen=True, slots=True)
class ListApplicationTagsInput:
    pass


setattr(ListApplicationTagsInput, '__module__', 'dex_connectors.jiguang.models')


# ApplicationTags contains names returned by the JPush tag list API.
@dataclass(frozen=True, slots=True)
class ApplicationTags:
    # Tags are tag names for the configured JPush application.
    tags: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tags', _replace_nil_slices(self.tags, 1))


ApplicationTags.__annotations__['tags'] = tuple[str, ...] | None
ApplicationTags.__dataclass_fields__['tags'].type = tuple[str, ...] | None
setattr(ApplicationTags, '__module__', 'dex_connectors.jiguang.models')
