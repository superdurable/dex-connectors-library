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


# ListApplicationsInput is empty because Tingyun lists all visible mobile apps.
@dataclass(frozen=True, slots=True)
class ListApplicationsInput:
    pass


setattr(ListApplicationsInput, '__module__', 'dex_connectors.tingyun.models')


# ApplicationMetadata identifies a mobile app without its secret appToken.
@dataclass(frozen=True, slots=True)
class ApplicationMetadata:
    # ID is Tingyun's numeric mobile application identifier.
    id: int = 0
    # Name is the mobile application display name.
    name: str = ''


setattr(ApplicationMetadata, '__module__', 'dex_connectors.tingyun.models')


# ListApplicationsOutput contains the mobile applications visible to the accessToken.
@dataclass(frozen=True, slots=True)
class ListApplicationsOutput:
    # Applications are the visible application identifiers and names.
    applications: tuple[ApplicationMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'applications', _replace_nil_slices(self.applications, 1))


ListApplicationsOutput.__annotations__['applications'] = tuple[ApplicationMetadata, ...] | None
ListApplicationsOutput.__dataclass_fields__['applications'].type = tuple[ApplicationMetadata, ...] | None
setattr(ListApplicationsOutput, '__module__', 'dex_connectors.tingyun.models')
