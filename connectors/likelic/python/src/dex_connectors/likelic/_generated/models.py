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


# ListRoomsInput selects a one-based page of at most twenty rooms.
@dataclass(frozen=True, slots=True)
class ListRoomsInput:
    # Page is a positive page number, starting at one.
    page: int = 0


setattr(ListRoomsInput, '__module__', 'dex_connectors.likelic.models')


# Room is one notification target with no group name, company, or member details.
@dataclass(frozen=True, slots=True)
class Room:
    # Channel is wework or qq.
    channel: str = ''
    # InstanceID identifies the provider robot instance for sending.
    instance_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'instanceID'})
    # RoomID is the provider group identifier.
    room_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'roomID'})
    # PushState is ready, blocked, or unknown.
    push_state: str = _dataclass_field(default='', metadata={'dex_json_name': 'pushState'})


setattr(Room, '__module__', 'dex_connectors.likelic.models')


# ListRoomsOutput contains one room page and the provider's total count.
@dataclass(frozen=True, slots=True)
class ListRoomsOutput:
    # Page is the requested one-based page.
    page: int = 0
    # Total is the number of matching provider rooms across all pages.
    total: int = 0
    # Rooms contains at most twenty room identifiers and states.
    rooms: tuple[Room, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'rooms', _replace_nil_slices(self.rooms, 1))


ListRoomsOutput.__annotations__['rooms'] = tuple[Room, ...] | None
ListRoomsOutput.__dataclass_fields__['rooms'].type = tuple[Room, ...] | None
setattr(ListRoomsOutput, '__module__', 'dex_connectors.likelic.models')
