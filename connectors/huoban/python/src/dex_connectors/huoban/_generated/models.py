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


# ListSpacesInput requires no selection because the API Key determines authorized workspaces.
@dataclass(frozen=True, slots=True)
class ListSpacesInput:
    pass


setattr(ListSpacesInput, '__module__', 'dex_connectors.huoban.models')


# SpaceMetadata identifies an authorized workspace without cover images or member data.
@dataclass(frozen=True, slots=True)
class SpaceMetadata:
    # SpaceID is the provider's opaque workspace identifier.
    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId'})
    # Name is the workspace display name.
    name: str = ''


setattr(SpaceMetadata, '__module__', 'dex_connectors.huoban.models')


# ListSpacesOutput contains only authorized workspace IDs and names.
@dataclass(frozen=True, slots=True)
class ListSpacesOutput:
    # Spaces are the workspaces returned for this API Key.
    spaces: tuple[SpaceMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'spaces', _replace_nil_slices(self.spaces, 1))


ListSpacesOutput.__annotations__['spaces'] = tuple[SpaceMetadata, ...] | None
ListSpacesOutput.__dataclass_fields__['spaces'].type = tuple[SpaceMetadata, ...] | None
setattr(ListSpacesOutput, '__module__', 'dex_connectors.huoban.models')
