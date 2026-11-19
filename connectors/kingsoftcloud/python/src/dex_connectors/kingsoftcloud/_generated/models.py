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


# ListUsersInput selects one IAM user page. MaxItems zero uses the provider default of 100.
@dataclass(frozen=True, slots=True)
class ListUsersInput:
    # Marker is the opaque next-page token from a previous response; empty starts at the first page.
    marker: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # MaxItems is the requested page size from 1 to 100; zero uses 100.
    max_items: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxItems', 'dex_json_omit_default': True})


setattr(ListUsersInput, '__module__', 'dex_connectors.kingsoftcloud.models')


# IAMUser contains only stable IAM identity fields, excluding contact and profile data.
@dataclass(frozen=True, slots=True)
class IAMUser:
    # UserID is the provider's stable IAM user ID.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})
    # UserName is the provider's IAM user name.
    user_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'userName'})


setattr(IAMUser, '__module__', 'dex_connectors.kingsoftcloud.models')


# ListUsersOutput contains one page and its optional continuation marker.
@dataclass(frozen=True, slots=True)
class ListUsersOutput:
    # Users contains the returned IAM identities.
    users: tuple[IAMUser, ...] = ()
    # HasMore reports whether another page is available.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    # NextMarker is passed as the next call's Marker when HasMore is true.
    next_marker: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextMarker'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'users', _replace_nil_slices(self.users, 1))


ListUsersOutput.__annotations__['users'] = tuple[IAMUser, ...] | None
ListUsersOutput.__dataclass_fields__['users'].type = tuple[IAMUser, ...] | None
setattr(ListUsersOutput, '__module__', 'dex_connectors.kingsoftcloud.models')
