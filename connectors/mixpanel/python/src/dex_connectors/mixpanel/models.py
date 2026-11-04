# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    EventImportResult as EventImportResult,
)
from ._generated.models import (
    ExportedEvent as ExportedEvent,
)
from ._generated.models import ExportedEvents as ExportedEvents
from ._generated.models import (
    ExportEventsInput as ExportEventsInput,
)
from ._generated.models import (
    TrackEventInput as TrackEventInput,
)

ExportedEvents.__doc__ = "Raw events in export order."

TrackEventInput.__doc__ = "Event name, distinct ID, Unix time in seconds, and optional string properties."
EventImportResult.__doc__ = "Whether Mixpanel imported the event."
ExportEventsInput.__doc__ = "Inclusive YYYY-MM-DD range of at most 30 days and a limit from 1 through 100."
ExportedEvent.__doc__ = "One raw event's name, distinct ID, Unix time, and insert ID."
