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


# ListAppsPageInput selects one page of authorized applications.
@dataclass(frozen=True, slots=True)
class ListAppsPageInput:
    # Skip omits this many earlier applications, starting at zero.
    skip: int = 0
    # Limit is the page size from 1 through 100; zero uses 100.
    limit: int = 0


setattr(ListAppsPageInput, '__module__', 'dex_connectors.fanruan.models')


# AppMetadata identifies one application without exposing form records.
@dataclass(frozen=True, slots=True)
class AppMetadata:
    # ID is the stable application ID.
    id: str = ''
    # Name is the application display name.
    name: str = ''


setattr(AppMetadata, '__module__', 'dex_connectors.fanruan.models')


# ListAppsPageOutput contains one page and a possible next offset.
@dataclass(frozen=True, slots=True)
class ListAppsPageOutput:
    # Apps are the applications visible to the API Key.
    apps: tuple[AppMetadata, ...] = ()
    # NextSkip is the next offset when this page is full; zero means no further page is suggested.
    next_skip: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextSkip'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'apps', _replace_nil_slices(self.apps, 1))


ListAppsPageOutput.__annotations__['apps'] = tuple[AppMetadata, ...] | None
ListAppsPageOutput.__dataclass_fields__['apps'].type = tuple[AppMetadata, ...] | None
setattr(ListAppsPageOutput, '__module__', 'dex_connectors.fanruan.models')
