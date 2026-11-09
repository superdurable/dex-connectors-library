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


# SearchUsersInput identifies a user by ID, device ID, Amplitude ID, or user ID prefix.
@dataclass(frozen=True, slots=True)
class SearchUsersInput:
    # User is the non-secret search term accepted by Amplitude User Search.
    user: str = ''


setattr(SearchUsersInput, '__module__', 'dex_connectors.amplitude.models')


# UserSearchPage contains the matched user IDs and Amplitude's match type.
@dataclass(frozen=True, slots=True)
class UserSearchPage:
    # User is the search term supplied to Amplitude.
    user: str = ''
    # UserIDs are the matched user IDs in provider order.
    user_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'userIDs'})
    # MatchType names the matching strategy reported by Amplitude.
    match_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'matchType'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'user_ids', _replace_nil_slices(self.user_ids, 1))


UserSearchPage.__annotations__['user_ids'] = tuple[str, ...] | None
UserSearchPage.__dataclass_fields__['user_ids'].type = tuple[str, ...] | None
setattr(UserSearchPage, '__module__', 'dex_connectors.amplitude.models')
