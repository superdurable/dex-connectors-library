# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListTrackedShipmentsInput, TrackedShipment, TrackedShipmentPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_TRACKED_SHIPMENTS_DEFINITION, PROVIDER_REJECTED

def list_tracked_shipments_listed(value: TrackedShipmentPage, failure: Failure | None = None) -> Case[TrackedShipmentPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_tracked_shipments_listed, '__module__', "dex_connectors.seventeentrack.seventeentrackmock")

def list_tracked_shipments_provider_rejected(value: TrackedShipmentPage, failure: Failure | None = None) -> Case[TrackedShipmentPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_tracked_shipments_provider_rejected, '__module__', "dex_connectors.seventeentrack.seventeentrackmock")

def list_tracked_shipments_invalid_response(value: TrackedShipmentPage, failure: Failure | None = None) -> Case[TrackedShipmentPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_tracked_shipments_invalid_response, '__module__', "dex_connectors.seventeentrack.seventeentrackmock")

def list_tracked_shipments_defect(value: TrackedShipmentPage, failure: Failure | None = None) -> Case[TrackedShipmentPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_tracked_shipments_defect, '__module__', "dex_connectors.seventeentrack.seventeentrackmock")

def list_tracked_shipments_retry(failure: Failure, after_seconds: float = 0) -> Case[TrackedShipmentPage]:
    return Case.retry(failure, after_seconds)

setattr(list_tracked_shipments_retry, '__module__', "dex_connectors.seventeentrack.seventeentrackmock")

def list_tracked_shipments_listed_shipment() -> Case[TrackedShipmentPage]:
    return Case.branch_result(LISTED, TrackedShipmentPage(page_number=1, total=1, shipments=(TrackedShipment(number='RR123456789CN', carrier=3011, package_status='InTransit', tracking_status='Tracking'),)))

setattr(list_tracked_shipments_listed_shipment, '__module__', "dex_connectors.seventeentrack.seventeentrackmock")


class Mock:

    def __init__(self, connection_name: str = "seventeentrack") -> None:
        self.list_tracked_shipments_script: Query[ListTrackedShipmentsInput, TrackedShipmentPage] = Query(LIST_TRACKED_SHIPMENTS_DEFINITION)
        self.list_tracked_shipments_script.default(list_tracked_shipments_listed_shipment())
        self._connection = new_connection_with_operations(
            list_tracked_shipments=self.list_tracked_shipments_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.seventeentrack.seventeentrackmock")

__all__ = [
    "Mock",
    "list_tracked_shipments_listed",
    "list_tracked_shipments_provider_rejected",
    "list_tracked_shipments_invalid_response",
    "list_tracked_shipments_defect",
    "list_tracked_shipments_retry",
    "list_tracked_shipments_listed_shipment",
]
