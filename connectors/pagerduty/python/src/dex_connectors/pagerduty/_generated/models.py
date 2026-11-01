# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass

@dataclass(frozen=True, slots=True)
class ListIncidentsInput:
    """Typed ListIncidentsInput value from the Go connector contract."""

    limit: int = 0
    offset: int = 0


setattr(ListIncidentsInput, '__module__', 'dex_connectors.pagerduty.models')


@dataclass(frozen=True, slots=True)
class Incident:
    """Typed Incident value from the Go connector contract."""

    id: str = ''
    title: str = ''
    status: str = ''


setattr(Incident, '__module__', 'dex_connectors.pagerduty.models')


@dataclass(frozen=True, slots=True)
class IncidentPage:
    """Typed IncidentPage value from the Go connector contract."""

    incidents: tuple[Incident, ...] | None = None
    limit: int = 0
    offset: int = 0
    more: bool = False


setattr(IncidentPage, '__module__', 'dex_connectors.pagerduty.models')


@dataclass(frozen=True, slots=True)
class GetIncidentInput:
    """Typed GetIncidentInput value from the Go connector contract."""

    id: str = ''


setattr(GetIncidentInput, '__module__', 'dex_connectors.pagerduty.models')
