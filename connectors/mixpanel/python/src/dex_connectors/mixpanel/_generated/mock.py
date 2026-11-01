# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import EventImportResult, ExportEventsInput, ExportedEvent, ExportedEvents, TrackEventInput
from .definitions import CONNECTOR_ID, DEFECT, EXPORTED, EXPORT_EVENTS_DEFINITION, IMPORTED, INVALID_RESPONSE, PROVIDER_REJECTED, TRACK_EVENT_DEFINITION

def track_event_imported(value: EventImportResult, failure: Failure | None = None) -> Case[EventImportResult]:
    return Case.branch_result(IMPORTED, value, failure)

setattr(track_event_imported, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def track_event_provider_rejected(value: EventImportResult, failure: Failure | None = None) -> Case[EventImportResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(track_event_provider_rejected, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def track_event_invalid_response(value: EventImportResult, failure: Failure | None = None) -> Case[EventImportResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(track_event_invalid_response, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def track_event_defect(value: EventImportResult, failure: Failure | None = None) -> Case[EventImportResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(track_event_defect, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def track_event_retry(failure: Failure, after_seconds: float = 0) -> Case[EventImportResult]:
    return Case.retry(failure, after_seconds)

setattr(track_event_retry, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def track_event_imported_default_event() -> Case[EventImportResult]:
    return Case.branch_result(IMPORTED, EventImportResult(imported=True))

setattr(track_event_imported_default_event, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def export_events_exported(value: ExportedEvents, failure: Failure | None = None) -> Case[ExportedEvents]:
    return Case.branch_result(EXPORTED, value, failure)

setattr(export_events_exported, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def export_events_provider_rejected(value: ExportedEvents, failure: Failure | None = None) -> Case[ExportedEvents]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(export_events_provider_rejected, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def export_events_invalid_response(value: ExportedEvents, failure: Failure | None = None) -> Case[ExportedEvents]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(export_events_invalid_response, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def export_events_defect(value: ExportedEvents, failure: Failure | None = None) -> Case[ExportedEvents]:
    return Case.branch_result(DEFECT, value, failure)

setattr(export_events_defect, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def export_events_retry(failure: Failure, after_seconds: float = 0) -> Case[ExportedEvents]:
    return Case.retry(failure, after_seconds)

setattr(export_events_retry, '__module__', "dex_connectors.mixpanel.mixpanelmock")

def export_events_exported_default_events() -> Case[ExportedEvents]:
    return Case.branch_result(EXPORTED, ExportedEvents(events=(ExportedEvent(event='Signed Up', distinct_id='member-1', time=1793232000, insert_id='4b5d3f33-2e21-4c1c-b392-2dae2a74a2ed'),)))

setattr(export_events_exported_default_events, '__module__', "dex_connectors.mixpanel.mixpanelmock")


class Mock:

    def __init__(self, connection_name: str = "mixpanel") -> None:
        self.track_event_script: Mutation[TrackEventInput, EventImportResult] = Mutation(TRACK_EVENT_DEFINITION)
        self.track_event_script.default(track_event_imported_default_event())
        self.export_events_script: Query[ExportEventsInput, ExportedEvents] = Query(EXPORT_EVENTS_DEFINITION)
        self.export_events_script.default(export_events_exported_default_events())
        self._connection = new_connection_with_operations(
            track_event=self.track_event_script,
            export_events=self.export_events_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.mixpanel.mixpanelmock")

__all__ = [
    "Mock",
    "track_event_imported",
    "track_event_provider_rejected",
    "track_event_invalid_response",
    "track_event_defect",
    "track_event_retry",
    "track_event_imported_default_event",
    "export_events_exported",
    "export_events_provider_rejected",
    "export_events_invalid_response",
    "export_events_defect",
    "export_events_retry",
    "export_events_exported_default_events",
]
