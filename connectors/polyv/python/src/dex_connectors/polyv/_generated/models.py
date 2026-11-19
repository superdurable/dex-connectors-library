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


# ListChannelsInput requests the complete live channel-ID list for the account.
@dataclass(frozen=True, slots=True)
class ListChannelsInput:
    pass


setattr(ListChannelsInput, '__module__', 'dex_connectors.polyv.models')


# ListChannelsOutput contains only provider channel IDs, with no channel configuration.
@dataclass(frozen=True, slots=True)
class ListChannelsOutput:
    channel_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'channelIds'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'channel_ids', _replace_nil_slices(self.channel_ids, 1))


ListChannelsOutput.__annotations__['channel_ids'] = tuple[str, ...] | None
ListChannelsOutput.__dataclass_fields__['channel_ids'].type = tuple[str, ...] | None
setattr(ListChannelsOutput, '__module__', 'dex_connectors.polyv.models')
