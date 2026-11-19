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


# ListApplicationsInput requires no selection because the accessToken determines the workspace.
@dataclass(frozen=True, slots=True)
class ListApplicationsInput:
    pass


setattr(ListApplicationsInput, '__module__', 'dex_connectors.qingflow.models')


# ApplicationMetadata identifies an application without its records or form contents.
@dataclass(frozen=True, slots=True)
class ApplicationMetadata:
    # AppKey is the application's opaque provider key.
    app_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'appKey'})
    # AppName is its display name.
    app_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'appName'})


setattr(ApplicationMetadata, '__module__', 'dex_connectors.qingflow.models')


# ListApplicationsOutput contains only application keys and names.
@dataclass(frozen=True, slots=True)
class ListApplicationsOutput:
    # Applications are the applications in the token's workspace.
    applications: tuple[ApplicationMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'applications', _replace_nil_slices(self.applications, 1))


ListApplicationsOutput.__annotations__['applications'] = tuple[ApplicationMetadata, ...] | None
ListApplicationsOutput.__dataclass_fields__['applications'].type = tuple[ApplicationMetadata, ...] | None
setattr(ListApplicationsOutput, '__module__', 'dex_connectors.qingflow.models')
