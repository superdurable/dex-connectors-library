# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import QueryWaybillTraceInput, WaybillEvent, WaybillTrace
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, QUERY_WAYBILL_TRACE_DEFINITION

def query_waybill_trace_found(value: WaybillTrace, failure: Failure | None = None) -> Case[WaybillTrace]:
    return Case.branch_result(FOUND, value, failure)

setattr(query_waybill_trace_found, '__module__', "dex_connectors.sto.stomock")

def query_waybill_trace_provider_rejected(value: WaybillTrace, failure: Failure | None = None) -> Case[WaybillTrace]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_waybill_trace_provider_rejected, '__module__', "dex_connectors.sto.stomock")

def query_waybill_trace_invalid_response(value: WaybillTrace, failure: Failure | None = None) -> Case[WaybillTrace]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_waybill_trace_invalid_response, '__module__', "dex_connectors.sto.stomock")

def query_waybill_trace_defect(value: WaybillTrace, failure: Failure | None = None) -> Case[WaybillTrace]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_waybill_trace_defect, '__module__', "dex_connectors.sto.stomock")

def query_waybill_trace_retry(failure: Failure, after_seconds: float = 0) -> Case[WaybillTrace]:
    return Case.retry(failure, after_seconds)

setattr(query_waybill_trace_retry, '__module__', "dex_connectors.sto.stomock")

def query_waybill_trace_found_default() -> Case[WaybillTrace]:
    return Case.branch_result(FOUND, WaybillTrace(waybill_no='884000400421364', events=(WaybillEvent(scan_type='收件', op_time='2020-02-18 17:20:08', op_org_name='青浦重固网点'),)))

setattr(query_waybill_trace_found_default, '__module__', "dex_connectors.sto.stomock")


class Mock:

    def __init__(self, connection_name: str = "sto") -> None:
        self.query_waybill_trace_script: Query[QueryWaybillTraceInput, WaybillTrace] = Query(QUERY_WAYBILL_TRACE_DEFINITION)
        self.query_waybill_trace_script.default(query_waybill_trace_found_default())
        self._connection = new_connection_with_operations(
            query_waybill_trace=self.query_waybill_trace_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.sto.stomock")

__all__ = [
    "Mock",
    "query_waybill_trace_found",
    "query_waybill_trace_provider_rejected",
    "query_waybill_trace_invalid_response",
    "query_waybill_trace_defect",
    "query_waybill_trace_retry",
    "query_waybill_trace_found_default",
]
