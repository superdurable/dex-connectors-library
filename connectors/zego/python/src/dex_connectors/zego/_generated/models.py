# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetRoomUserCountInput identifies a ZIM room created by the application.
@dataclass(frozen=True, slots=True)
class GetRoomUserCountInput:
    # RoomID is the application-assigned ZIM room ID, at most 32 supported ASCII bytes.
    room_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'roomId'})


setattr(GetRoomUserCountInput, '__module__', 'dex_connectors.zego.models')


# GetRoomUserCountOutput contains only the current member count.
@dataclass(frozen=True, slots=True)
class GetRoomUserCountOutput:
    # Count is the current number of users in the room.
    count: int = 0


setattr(GetRoomUserCountOutput, '__module__', 'dex_connectors.zego.models')
