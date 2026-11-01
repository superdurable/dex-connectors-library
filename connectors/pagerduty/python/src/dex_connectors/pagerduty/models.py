# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetIncidentInput as GetIncidentInput,
)
from ._generated.models import (
    Incident as Incident,
)
from ._generated.models import IncidentPage as IncidentPage
from ._generated.models import (
    ListIncidentsInput as ListIncidentsInput,
)

IncidentPage.__doc__ = "One offset page of incidents; more reports whether another page exists."
