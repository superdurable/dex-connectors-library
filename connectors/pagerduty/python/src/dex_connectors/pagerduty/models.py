# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed PagerDuty incident pages and incident records."""

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class ListIncidentsInput:
    """One page of incidents; limit zero means 25, at most 100."""

    limit: int = 0
    offset: int = 0


@dataclass(frozen=True, slots=True)
class GetIncidentInput:
    """PagerDuty incident ID, such as P123456."""

    id: str = ""


@dataclass(frozen=True, slots=True)
class Incident:
    """Incident summary with PagerDuty's status text, such as triggered or resolved."""

    id: str = ""
    title: str = ""
    status: str = ""


@dataclass(frozen=True, slots=True)
class IncidentPage:
    """One offset page of incidents; more reports whether another page exists."""

    incidents: tuple[Incident, ...] = ()
    limit: int = 0
    offset: int = 0
    more: bool = False
