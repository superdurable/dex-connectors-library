# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

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
    incidents: tuple[Incident, ...] | None = None
    # Limit is the maximum number of incidents requested.
    limit: int = 0
    # Offset is the zero-based page offset.
    offset: int = 0
    # More reports whether another incident page exists.
    more: bool = False


setattr(IncidentPage, '__module__', 'dex_connectors.pagerduty.models')


# GetIncidentInput identifies an incident.
@dataclass(frozen=True, slots=True)
class GetIncidentInput:
    # ID is the provider's stable resource identifier.
    id: str = ''


setattr(GetIncidentInput, '__module__', 'dex_connectors.pagerduty.models')
