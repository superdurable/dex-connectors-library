# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListTopicsInput requests the first page when NextToken is empty.
@dataclass(frozen=True, slots=True)
class ListTopicsInput:
    next_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextToken'})


setattr(ListTopicsInput, '__module__', 'dex_connectors.amazon_sns.models')


# TopicPage contains up to 100 topic ARNs and a continuation token.
@dataclass(frozen=True, slots=True)
class TopicPage:
    topic_ar_ns: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'topicARNs'})
    next_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'topic_ar_ns', _replace_nil_slices(self.topic_ar_ns, 1))


TopicPage.__annotations__['topic_ar_ns'] = tuple[str, ...] | None
TopicPage.__dataclass_fields__['topic_ar_ns'].type = tuple[str, ...] | None
setattr(TopicPage, '__module__', 'dex_connectors.amazon_sns.models')
