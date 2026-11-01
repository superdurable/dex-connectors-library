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

ListIncidentsInput.__doc__ = "One page of incidents; limit zero means 25, at most 100."
GetIncidentInput.__doc__ = "PagerDuty incident ID, such as P123456."
Incident.__doc__ = "Incident summary with PagerDuty's status text, such as triggered or resolved."
