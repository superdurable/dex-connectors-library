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


# ListRoomsInput requests the rooms visible to the administrator derived from the token.
@dataclass(frozen=True, slots=True)
class ListRoomsInput:
    pass


setattr(ListRoomsInput, '__module__', 'dex_connectors.quanshi.models')


# RoomSummary contains only operational room metadata.
@dataclass(frozen=True, slots=True)
class RoomSummary:
    # ID is the stable meeting room identifier.
    id: int = 0
    # Name is the meeting room name.
    name: str = ''
    # Capacity is the number of people supported by the room.
    capacity: int = 0
    # Status is the provider's roomStatus numeric value.
    status: int = 0


setattr(RoomSummary, '__module__', 'dex_connectors.quanshi.models')


# ListRoomsOutput contains the returned meeting rooms.
@dataclass(frozen=True, slots=True)
class ListRoomsOutput:
    # Rooms are returned in provider order.
    rooms: tuple[RoomSummary, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'rooms', _replace_nil_slices(self.rooms, 1))


ListRoomsOutput.__annotations__['rooms'] = tuple[RoomSummary, ...] | None
ListRoomsOutput.__dataclass_fields__['rooms'].type = tuple[RoomSummary, ...] | None
setattr(ListRoomsOutput, '__module__', 'dex_connectors.quanshi.models')
