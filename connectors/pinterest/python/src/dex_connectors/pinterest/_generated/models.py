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


# ListPinsInput selects one cursor-based page of owned Pins.
@dataclass(frozen=True, slots=True)
class ListPinsInput:
    # PageSize is the maximum count from 1 to 250; zero uses the provider default of 25.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Bookmark is the cursor returned by a previous PinsPage; blank starts at the first page.
    bookmark: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListPinsInput, '__module__', 'dex_connectors.pinterest.models')


# Pin contains a stable Pinterest identity and its display title.
@dataclass(frozen=True, slots=True)
class Pin:
    # ID is the provider's stable numeric Pin ID.
    id: str = ''
    # BoardID is the numeric ID of the containing board.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId'})
    # Title is the Pin title, if supplied by Pinterest.
    title: str = ''


setattr(Pin, '__module__', 'dex_connectors.pinterest.models')


# PinsPage contains the selected Pins and the cursor for the next page.
@dataclass(frozen=True, slots=True)
class PinsPage:
    # Pins are the identities returned by this page request.
    pins: tuple[Pin, ...] = ()
    # NextBookmark is blank when no further page exists.
    next_bookmark: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextBookmark'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'pins', _replace_nil_slices(self.pins, 1))


PinsPage.__annotations__['pins'] = tuple[Pin, ...] | None
PinsPage.__dataclass_fields__['pins'].type = tuple[Pin, ...] | None
setattr(PinsPage, '__module__', 'dex_connectors.pinterest.models')
