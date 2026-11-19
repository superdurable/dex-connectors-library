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


# ListTeamsInput selects a one-based team page; zero page and size use 1 and 20.
@dataclass(frozen=True, slots=True)
class ListTeamsInput:
    # Page is the one-based page number.
    page: int = 0
    # PageSize is the number of teams to request, from 1 to 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListTeamsInput, '__module__', 'dex_connectors.rainbond.models')


# Team contains only non-personal Rainbond team metadata.
@dataclass(frozen=True, slots=True)
class Team:
    # ID is the Rainbond tenant_id.
    id: str = ''
    # Name is the Rainbond tenant_name.
    name: str = ''
    # Active reports whether the team is active.
    active: bool = False


setattr(Team, '__module__', 'dex_connectors.rainbond.models')


# ListTeamsOutput contains one team page and the total count.
@dataclass(frozen=True, slots=True)
class ListTeamsOutput:
    # Teams are the returned teams in provider order.
    teams: tuple[Team, ...] = ()
    # Total is the total number of accessible teams.
    total: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'teams', _replace_nil_slices(self.teams, 1))


ListTeamsOutput.__annotations__['teams'] = tuple[Team, ...] | None
ListTeamsOutput.__dataclass_fields__['teams'].type = tuple[Team, ...] | None
setattr(ListTeamsOutput, '__module__', 'dex_connectors.rainbond.models')
