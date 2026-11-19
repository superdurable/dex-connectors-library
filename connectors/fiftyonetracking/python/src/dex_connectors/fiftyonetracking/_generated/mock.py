# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetShipmentStatusCountsInput, ShipmentStatusCounts
from .definitions import CONNECTOR_ID, COUNTED, DEFECT, GET_SHIPMENT_STATUS_COUNTS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_shipment_status_counts_counted(value: ShipmentStatusCounts, failure: Failure | None = None) -> Case[ShipmentStatusCounts]:
    return Case.branch_result(COUNTED, value, failure)

setattr(get_shipment_status_counts_counted, '__module__', "dex_connectors.fiftyonetracking.fiftyonetrackingmock")

def get_shipment_status_counts_provider_rejected(value: ShipmentStatusCounts, failure: Failure | None = None) -> Case[ShipmentStatusCounts]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_shipment_status_counts_provider_rejected, '__module__', "dex_connectors.fiftyonetracking.fiftyonetrackingmock")

def get_shipment_status_counts_invalid_response(value: ShipmentStatusCounts, failure: Failure | None = None) -> Case[ShipmentStatusCounts]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_shipment_status_counts_invalid_response, '__module__', "dex_connectors.fiftyonetracking.fiftyonetrackingmock")

def get_shipment_status_counts_defect(value: ShipmentStatusCounts, failure: Failure | None = None) -> Case[ShipmentStatusCounts]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_shipment_status_counts_defect, '__module__', "dex_connectors.fiftyonetracking.fiftyonetrackingmock")

def get_shipment_status_counts_retry(failure: Failure, after_seconds: float = 0) -> Case[ShipmentStatusCounts]:
    return Case.retry(failure, after_seconds)

setattr(get_shipment_status_counts_retry, '__module__', "dex_connectors.fiftyonetracking.fiftyonetrackingmock")

def get_shipment_status_counts_counted_parcels() -> Case[ShipmentStatusCounts]:
    return Case.branch_result(COUNTED, ShipmentStatusCounts(pending=1, not_found=2, in_transit=3, pickup=4, delivered=5, expired=0, undelivered=0, exception=0))

setattr(get_shipment_status_counts_counted_parcels, '__module__', "dex_connectors.fiftyonetracking.fiftyonetrackingmock")


class Mock:

    def __init__(self, connection_name: str = "fiftyonetracking") -> None:
        self.get_shipment_status_counts_script: Query[GetShipmentStatusCountsInput, ShipmentStatusCounts] = Query(GET_SHIPMENT_STATUS_COUNTS_DEFINITION)
        self.get_shipment_status_counts_script.default(get_shipment_status_counts_counted_parcels())
        self._connection = new_connection_with_operations(
            get_shipment_status_counts=self.get_shipment_status_counts_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.fiftyonetracking.fiftyonetrackingmock")

__all__ = [
    "Mock",
    "get_shipment_status_counts_counted",
    "get_shipment_status_counts_provider_rejected",
    "get_shipment_status_counts_invalid_response",
    "get_shipment_status_counts_defect",
    "get_shipment_status_counts_retry",
    "get_shipment_status_counts_counted_parcels",
]
