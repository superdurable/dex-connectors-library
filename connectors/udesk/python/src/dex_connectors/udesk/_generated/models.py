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


# ListTicketStatusesInput optionally filters by Udesk's enable flag.
@dataclass(frozen=True, slots=True)
class ListTicketStatusesInput:
    # EnabledOnly is omitted to list every status; true or false filters by enabled state.
    enabled_only: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'enabledOnly', 'dex_json_omit_default': True})


setattr(ListTicketStatusesInput, '__module__', 'dex_connectors.udesk.models')


# TicketStatus is one account-defined Udesk ticket status.
@dataclass(frozen=True, slots=True)
class TicketStatus:
    # StatusID is the stable Udesk status identifier.
    status_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'statusId'})
    # Name is the Chinese display name, which can be edited for solving substates.
    name: str = ''
    # EnglishName is the provider-generated English name.
    english_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'englishName'})
    # Enabled reports whether this status is available to users.
    enabled: bool = False
    # Color is the provider's color value.
    color: str = ''


setattr(TicketStatus, '__module__', 'dex_connectors.udesk.models')


# ListTicketStatusesOutput contains zero or more ticket statuses.
@dataclass(frozen=True, slots=True)
class ListTicketStatusesOutput:
    # Statuses is empty when no statuses match.
    statuses: tuple[TicketStatus, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'statuses', _replace_nil_slices(self.statuses, 1))


ListTicketStatusesOutput.__annotations__['statuses'] = tuple[TicketStatus, ...] | None
ListTicketStatusesOutput.__dataclass_fields__['statuses'].type = tuple[TicketStatus, ...] | None
setattr(ListTicketStatusesOutput, '__module__', 'dex_connectors.udesk.models')
