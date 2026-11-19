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


# ListSpacesInput requires no caller-provided selector; the token identifies the user.
@dataclass(frozen=True, slots=True)
class ListSpacesInput:
    pass


setattr(ListSpacesInput, '__module__', 'dex_connectors.vika.models')


# Space is a Vika workspace the authorized user created or joined.
@dataclass(frozen=True, slots=True)
class Space:
    # ID is the stable Vika workspace ID.
    id: str = ''
    # Name is the workspace display name.
    name: str = ''
    # IsAdmin reports whether the authorized user is its primary administrator.
    is_admin: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAdmin'})


setattr(Space, '__module__', 'dex_connectors.vika.models')


# SpacePage contains all workspaces returned by Vika's unpaginated endpoint.
@dataclass(frozen=True, slots=True)
class SpacePage:
    # Spaces contains only workspace metadata and no datasheet records.
    spaces: tuple[Space, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'spaces', _replace_nil_slices(self.spaces, 1))


SpacePage.__annotations__['spaces'] = tuple[Space, ...] | None
SpacePage.__dataclass_fields__['spaces'].type = tuple[Space, ...] | None
setattr(SpacePage, '__module__', 'dex_connectors.vika.models')
