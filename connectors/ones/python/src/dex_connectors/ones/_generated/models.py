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


# ListTeamsInput has no arguments; the API key identifies the accessible teams.
@dataclass(frozen=True, slots=True)
class ListTeamsInput:
    pass


setattr(ListTeamsInput, '__module__', 'dex_connectors.ones.models')


# Team contains non-personal team metadata.
@dataclass(frozen=True, slots=True)
class Team:
    # ID is the ONES team UUID.
    id: str = ''
    # Name is the team name.
    name: str = ''
    # Status is the provider's numeric team status.
    status: int = 0


setattr(Team, '__module__', 'dex_connectors.ones.models')


# ListTeamsOutput contains teams accessible to the API key.
@dataclass(frozen=True, slots=True)
class ListTeamsOutput:
    # Teams are the accessible teams in provider order.
    teams: tuple[Team, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'teams', _replace_nil_slices(self.teams, 1))


ListTeamsOutput.__annotations__['teams'] = tuple[Team, ...] | None
ListTeamsOutput.__dataclass_fields__['teams'].type = tuple[Team, ...] | None
setattr(ListTeamsOutput, '__module__', 'dex_connectors.ones.models')
