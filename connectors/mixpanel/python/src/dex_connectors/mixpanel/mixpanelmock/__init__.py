# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Mutation as Mutation
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import export_events_exported as export_events_exported
from .._generated.mock import export_events_exported_default_events as _export_events_exported_default_events
from .._generated.mock import track_event_imported as track_event_imported
from .._generated.mock import track_event_imported_default_event as _track_event_imported_default_event
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import EXPORT_EVENTS_DEFINITION as EXPORT_EVENTS_DEFINITION
from ..client import EXPORTED as EXPORTED
from ..client import IMPORTED as IMPORTED
from ..client import TRACK_EVENT_DEFINITION as TRACK_EVENT_DEFINITION
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import EventImportResult as EventImportResult
from ..models import EventImportResult as _EventImportResult
from ..models import ExportedEvent as ExportedEvent
from ..models import ExportedEvents as ExportedEvents
from ..models import ExportedEvents as _ExportedEvents
from ..models import ExportEventsInput as ExportEventsInput
from ..models import TrackEventInput as TrackEventInput


def export_events_default_answer() -> _Case[_ExportedEvents]:
    """Return the manifest's exportEvents default answer."""
    return _export_events_exported_default_events()


def track_event_default_answer() -> _Case[_EventImportResult]:
    """Return the manifest's trackEvent default answer."""
    return _track_event_imported_default_event()


__all__ = [
    "Mock",
    "track_event_imported",
    "track_event_default_answer",
    "export_events_exported",
    "export_events_default_answer",
]
