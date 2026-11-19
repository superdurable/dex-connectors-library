# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import QueryTrackingInput, QueryTrackingOutput, TrackingEvent
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED, QUERY_TRACKING_DEFINITION, RETRIEVED

def query_tracking_retrieved(value: QueryTrackingOutput, failure: Failure | None = None) -> Case[QueryTrackingOutput]:
    return Case.branch_result(RETRIEVED, value, failure)

setattr(query_tracking_retrieved, '__module__', "dex_connectors.kuaidibird.kuaidibirdmock")

def query_tracking_provider_rejected(value: QueryTrackingOutput, failure: Failure | None = None) -> Case[QueryTrackingOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_tracking_provider_rejected, '__module__', "dex_connectors.kuaidibird.kuaidibirdmock")

def query_tracking_invalid_response(value: QueryTrackingOutput, failure: Failure | None = None) -> Case[QueryTrackingOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_tracking_invalid_response, '__module__', "dex_connectors.kuaidibird.kuaidibirdmock")

def query_tracking_defect(value: QueryTrackingOutput, failure: Failure | None = None) -> Case[QueryTrackingOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_tracking_defect, '__module__', "dex_connectors.kuaidibird.kuaidibirdmock")

def query_tracking_retry(failure: Failure, after_seconds: float = 0) -> Case[QueryTrackingOutput]:
    return Case.retry(failure, after_seconds)

setattr(query_tracking_retry, '__module__', "dex_connectors.kuaidibird.kuaidibirdmock")

def query_tracking_sample_tracking() -> Case[QueryTrackingOutput]:
    return Case.branch_result(RETRIEVED, QueryTrackingOutput(carrier_code='YTO', tracking_number='YT1234567890123', state='2', events=(TrackingEvent(time='2026-11-12 15:30:00', content='Parcel was collected by the carrier.'), TrackingEvent(time='2026-11-13 10:00:00', content='Parcel reached the destination sorting center.'))))

setattr(query_tracking_sample_tracking, '__module__', "dex_connectors.kuaidibird.kuaidibirdmock")


class Mock:

    def __init__(self, connection_name: str = "kuaidibird") -> None:
        self.query_tracking_script: Query[QueryTrackingInput, QueryTrackingOutput] = Query(QUERY_TRACKING_DEFINITION)
        self.query_tracking_script.default(query_tracking_sample_tracking())
        self._connection = new_connection_with_operations(
            query_tracking=self.query_tracking_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.kuaidibird.kuaidibirdmock")

__all__ = [
    "Mock",
    "query_tracking_retrieved",
    "query_tracking_provider_rejected",
    "query_tracking_invalid_response",
    "query_tracking_defect",
    "query_tracking_retry",
    "query_tracking_sample_tracking",
]
