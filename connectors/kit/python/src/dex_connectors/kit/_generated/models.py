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


# ListSubscribersInput selects a cursor page of up to 1000 subscribers.
@dataclass(frozen=True, slots=True)
class ListSubscribersInput:
    # After is the previous page's end cursor; blank starts from the first page.
    after: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PerPage is the maximum count; zero defaults to 20.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(ListSubscribersInput, '__module__', 'dex_connectors.kit.models')


# SubscriberMetadata contains subscriber identity without custom fields.
@dataclass(frozen=True, slots=True)
class SubscriberMetadata:
    # ID is Kit's stable numeric subscriber ID.
    id: int = 0
    # EmailAddress is the subscriber email address.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})
    # FirstName is the given name, when present.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # State is Kit's current subscriber lifecycle state.
    state: str = ''


setattr(SubscriberMetadata, '__module__', 'dex_connectors.kit.models')


# SubscribersPage contains one cursor page of subscriber metadata.
@dataclass(frozen=True, slots=True)
class SubscribersPage:
    # Subscribers contains the returned subscriber records.
    subscribers: tuple[SubscriberMetadata, ...] = ()
    # HasNextPage reports whether the end cursor can fetch another page.
    has_next_page: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasNextPage'})
    # NextCursor is the next page cursor when HasNextPage is true.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})
    # PerPage is the requested maximum count.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'subscribers', _replace_nil_slices(self.subscribers, 1))


SubscribersPage.__annotations__['subscribers'] = tuple[SubscriberMetadata, ...] | None
SubscribersPage.__dataclass_fields__['subscribers'].type = tuple[SubscriberMetadata, ...] | None
setattr(SubscribersPage, '__module__', 'dex_connectors.kit.models')


# GetSubscriberInput selects one numeric Kit subscriber ID.
@dataclass(frozen=True, slots=True)
class GetSubscriberInput:
    # SubscriberID is the ID from a subscriber page or API.
    subscriber_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'subscriberId'})


setattr(GetSubscriberInput, '__module__', 'dex_connectors.kit.models')
