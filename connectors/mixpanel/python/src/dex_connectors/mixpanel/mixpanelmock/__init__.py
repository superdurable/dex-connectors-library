# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Mixpanel scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import CONNECTOR_ID, EXPORT_EVENTS_DEFINITION, EXPORTED, IMPORTED, TRACK_EVENT_DEFINITION
from ..connection import Connection, new_connection_with_operations
from ..models import EventImportResult, ExportedEvent, ExportedEvents, ExportEventsInput, TrackEventInput


def track_event_imported(value: EventImportResult, failure: Failure | None = None) -> Case[EventImportResult]:
    """Script the imported route."""
    return Case.branch_result(IMPORTED, value, failure)


def track_event_default_answer() -> Case[EventImportResult]:
    """Return the manifest's trackEvent default answer."""
    return track_event_imported(EventImportResult(True))


def export_events_exported(value: ExportedEvents, failure: Failure | None = None) -> Case[ExportedEvents]:
    """Script the exported route."""
    return Case.branch_result(EXPORTED, value, failure)


def export_events_default_answer() -> Case[ExportedEvents]:
    """Return the manifest's exportEvents default answer."""
    return export_events_exported(
        ExportedEvents(
            (ExportedEvent("Signed Up", "member-1", 1793232000, "4b5d3f33-2e21-4c1c-b392-2dae2a74a2ed"),)
        )
    )


class Mock:
    """Script Mixpanel operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "mixpanel") -> None:
        self.track_event_script: Mutation[TrackEventInput, EventImportResult] = Mutation(
            TRACK_EVENT_DEFINITION
        )
        self.track_event_script.default(track_event_default_answer())
        self.export_events_script: Query[ExportEventsInput, ExportedEvents] = Query(EXPORT_EVENTS_DEFINITION)
        self.export_events_script.default(export_events_default_answer())
        self._connection = new_connection_with_operations(
            self.track_event_script, self.export_events_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "track_event_imported",
    "track_event_default_answer",
    "export_events_exported",
    "export_events_default_answer",
]
