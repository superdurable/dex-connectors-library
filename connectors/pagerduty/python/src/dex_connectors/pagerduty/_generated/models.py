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


# ListIncidentsInput chooses one incident page.
@dataclass(frozen=True, slots=True)
class ListIncidentsInput:
    # Limit is the maximum number of incidents requested.
    limit: int = 0
    # Offset is the zero-based page offset.
    offset: int = 0


setattr(ListIncidentsInput, '__module__', 'dex_connectors.pagerduty.models')


# Incident contains selected incident fields.
@dataclass(frozen=True, slots=True)
class Incident:
    # ID is the provider's stable resource identifier.
    id: str = ''
    # Title is the provider's display title.
    title: str = ''
    # Status is the provider's current status.
    status: str = ''


setattr(Incident, '__module__', 'dex_connectors.pagerduty.models')


# IncidentPage is one page of incidents.
@dataclass(frozen=True, slots=True)
class IncidentPage:
    # Incidents contains one incident page.
    incidents: tuple[Incident, ...] = ()
    # Limit is the maximum number of incidents requested.
    limit: int = 0
    # Offset is the zero-based page offset.
    offset: int = 0
    # More reports whether another incident page exists.
    more: bool = False

    def __post_init__(self) -> None:
        object.__setattr__(self, 'incidents', _replace_nil_slices(self.incidents, 1))


IncidentPage.__annotations__['incidents'] = tuple[Incident, ...] | None
IncidentPage.__dataclass_fields__['incidents'].type = tuple[Incident, ...] | None
setattr(IncidentPage, '__module__', 'dex_connectors.pagerduty.models')


# GetIncidentInput identifies an incident.
@dataclass(frozen=True, slots=True)
class GetIncidentInput:
    # ID is the provider's stable resource identifier.
    id: str = ''


setattr(GetIncidentInput, '__module__', 'dex_connectors.pagerduty.models')
