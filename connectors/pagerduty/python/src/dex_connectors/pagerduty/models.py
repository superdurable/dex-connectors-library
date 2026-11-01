# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    GetIncidentInput as GetIncidentInput,
)
from ._generated.models import (
    Incident as Incident,
)
from ._generated.models import (
    ListIncidentsInput as ListIncidentsInput,
)


@dataclass(frozen=True, slots=True)
class IncidentPage:
    """One offset page of incidents; more reports whether another page exists."""

    incidents: tuple[Incident, ...] = ()
    limit: int = 0
    offset: int = 0
    more: bool = False
