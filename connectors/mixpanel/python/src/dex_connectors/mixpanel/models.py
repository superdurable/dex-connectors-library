# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    EventImportResult as EventImportResult,
)
from ._generated.models import (
    ExportedEvent as ExportedEvent,
)
from ._generated.models import (
    ExportEventsInput as ExportEventsInput,
)
from ._generated.models import (
    TrackEventInput as TrackEventInput,
)


@dataclass(frozen=True, slots=True)
class ExportedEvents:
    """Raw events in export order."""

    events: tuple[ExportedEvent, ...] = ()
