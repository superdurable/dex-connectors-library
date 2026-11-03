# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import FieldEqualityFilter, GetRecordInput, ListRecordsInput, Record, RecordPage, RecordSort, RecordUpdate, RecordUpsert, UpdateRecordsInput, UpdatedRecords, UpsertRecordsInput, UpsertedRecords
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_RECORD_DEFINITION, INVALID_RESPONSE, LISTED, LIST_RECORDS_DEFINITION, NOT_FOUND, OFFSET_EXPIRED, PROVIDER_REJECTED, UPDATED, UPDATE_RECORDS_DEFINITION, UPSERTED, UPSERT_RECORDS_DEFINITION

def list_records_listed(value: RecordPage, failure: Failure | None = None) -> Case[RecordPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_records_listed, '__module__', "dex_connectors.airtable.airtablemock")

def list_records_offset_expired(value: RecordPage, failure: Failure | None = None) -> Case[RecordPage]:
    return Case.branch_result(OFFSET_EXPIRED, value, failure)

setattr(list_records_offset_expired, '__module__', "dex_connectors.airtable.airtablemock")

def list_records_provider_rejected(value: RecordPage, failure: Failure | None = None) -> Case[RecordPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_records_provider_rejected, '__module__', "dex_connectors.airtable.airtablemock")

def list_records_invalid_response(value: RecordPage, failure: Failure | None = None) -> Case[RecordPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_records_invalid_response, '__module__', "dex_connectors.airtable.airtablemock")

def list_records_defect(value: RecordPage, failure: Failure | None = None) -> Case[RecordPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_records_defect, '__module__', "dex_connectors.airtable.airtablemock")

def list_records_retry(failure: Failure, after_seconds: float = 0) -> Case[RecordPage]:
    return Case.retry(failure, after_seconds)

setattr(list_records_retry, '__module__', "dex_connectors.airtable.airtablemock")

def list_records_standard_refund_policy() -> Case[RecordPage]:
    return Case.paged(LISTED, (RecordPage(records=(Record(id='recPolicyStandard', created_time=datetime.fromisoformat("2026-09-01T09:00:00Z".replace('Z', '+00:00')), fields={'Policy Key': 'refund-standard', 'Approval Limit USD': 250}),), offset='policies-page-2', filter_formula='{Policy Key} = "refund-standard"'), RecordPage(records=(Record(id='recPolicyStandard', created_time=datetime.fromisoformat("2026-09-01T09:00:00Z".replace('Z', '+00:00')), fields={'Policy Key': 'refund-standard', 'Approval Limit USD': 250}),), offset='policies-page-3', filter_formula='{Policy Key} = "refund-standard"'), RecordPage(records=(), offset='', filter_formula='{Policy Key} = "refund-standard"')))

setattr(list_records_standard_refund_policy, '__module__', "dex_connectors.airtable.airtablemock")

def list_records_expired_policy_offset() -> Case[RecordPage]:
    return Case.branch_result(OFFSET_EXPIRED, RecordPage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listRecords", "Airtable no longer recognizes the list offset."))

setattr(list_records_expired_policy_offset, '__module__', "dex_connectors.airtable.airtablemock")

def list_records_policy_list_denied() -> Case[RecordPage]:
    return Case.branch_result(PROVIDER_REJECTED, RecordPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listRecords", "Airtable denied the policy listing."))

setattr(list_records_policy_list_denied, '__module__', "dex_connectors.airtable.airtablemock")

def get_record_found(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_record_found, '__module__', "dex_connectors.airtable.airtablemock")

def get_record_not_found(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_record_not_found, '__module__', "dex_connectors.airtable.airtablemock")

def get_record_provider_rejected(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_record_provider_rejected, '__module__', "dex_connectors.airtable.airtablemock")

def get_record_invalid_response(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_record_invalid_response, '__module__', "dex_connectors.airtable.airtablemock")

def get_record_defect(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_record_defect, '__module__', "dex_connectors.airtable.airtablemock")

def get_record_retry(failure: Failure, after_seconds: float = 0) -> Case[Record]:
    return Case.retry(failure, after_seconds)

setattr(get_record_retry, '__module__', "dex_connectors.airtable.airtablemock")

def get_record_approved_decision_log() -> Case[Record]:
    return Case.branch_result(FOUND, Record(id='recDecisionLog001', created_time=datetime.fromisoformat("2026-10-25T09:01:00Z".replace('Z', '+00:00')), fields={'Case ID': 'case-approved', 'Customer': 'Jane Doe', 'Amount USD': 120, 'Decision': 'approved', 'Policy': ['recPolicyStandard']}))

setattr(get_record_approved_decision_log, '__module__', "dex_connectors.airtable.airtablemock")

def get_record_missing_decision_log() -> Case[Record]:
    return Case.branch_result(NOT_FOUND, Record(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getRecord", "Airtable found no such record."))

setattr(get_record_missing_decision_log, '__module__', "dex_connectors.airtable.airtablemock")

def get_record_decision_read_denied() -> Case[Record]:
    return Case.branch_result(PROVIDER_REJECTED, Record(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getRecord", "Airtable denied the record read."))

setattr(get_record_decision_read_denied, '__module__', "dex_connectors.airtable.airtablemock")

def upsert_records_upserted(value: UpsertedRecords, failure: Failure | None = None) -> Case[UpsertedRecords]:
    return Case.branch_result(UPSERTED, value, failure)

setattr(upsert_records_upserted, '__module__', "dex_connectors.airtable.airtablemock")

def upsert_records_provider_rejected(value: UpsertedRecords, failure: Failure | None = None) -> Case[UpsertedRecords]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upsert_records_provider_rejected, '__module__', "dex_connectors.airtable.airtablemock")

def upsert_records_invalid_response(value: UpsertedRecords, failure: Failure | None = None) -> Case[UpsertedRecords]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upsert_records_invalid_response, '__module__', "dex_connectors.airtable.airtablemock")

def upsert_records_defect(value: UpsertedRecords, failure: Failure | None = None) -> Case[UpsertedRecords]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upsert_records_defect, '__module__', "dex_connectors.airtable.airtablemock")

def upsert_records_retry(failure: Failure, after_seconds: float = 0) -> Case[UpsertedRecords]:
    return Case.retry(failure, after_seconds)

setattr(upsert_records_retry, '__module__', "dex_connectors.airtable.airtablemock")

def upsert_records_logged_approved_refund() -> Case[UpsertedRecords]:
    return Case.branch_result(UPSERTED, UpsertedRecords(records=(Record(id='recDecisionLog001', created_time=datetime.fromisoformat("2026-10-25T09:01:00Z".replace('Z', '+00:00')), fields={'Case ID': 'case-approved', 'Customer': 'Jane Doe', 'Amount USD': 120, 'Decision': 'approved', 'Policy': ['recPolicyStandard']}),), created_record_ids=('recDecisionLog001',), updated_record_ids=()))

setattr(upsert_records_logged_approved_refund, '__module__', "dex_connectors.airtable.airtablemock")

def upsert_records_decision_upsert_denied() -> Case[UpsertedRecords]:
    return Case.branch_result(PROVIDER_REJECTED, UpsertedRecords(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "upsertRecords", "Airtable denied the decision log upsert."))

setattr(upsert_records_decision_upsert_denied, '__module__', "dex_connectors.airtable.airtablemock")

def update_records_updated(value: UpdatedRecords, failure: Failure | None = None) -> Case[UpdatedRecords]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_records_updated, '__module__', "dex_connectors.airtable.airtablemock")

def update_records_not_found(value: UpdatedRecords, failure: Failure | None = None) -> Case[UpdatedRecords]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_records_not_found, '__module__', "dex_connectors.airtable.airtablemock")

def update_records_provider_rejected(value: UpdatedRecords, failure: Failure | None = None) -> Case[UpdatedRecords]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_records_provider_rejected, '__module__', "dex_connectors.airtable.airtablemock")

def update_records_invalid_response(value: UpdatedRecords, failure: Failure | None = None) -> Case[UpdatedRecords]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_records_invalid_response, '__module__', "dex_connectors.airtable.airtablemock")

def update_records_defect(value: UpdatedRecords, failure: Failure | None = None) -> Case[UpdatedRecords]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_records_defect, '__module__', "dex_connectors.airtable.airtablemock")

def update_records_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdatedRecords]:
    return Case.retry(failure, after_seconds)

setattr(update_records_retry, '__module__', "dex_connectors.airtable.airtablemock")

def update_records_stamped_standard_policy() -> Case[UpdatedRecords]:
    return Case.branch_result(UPDATED, UpdatedRecords(records=(Record(id='recPolicyStandard', created_time=datetime.fromisoformat("2026-09-01T09:00:00Z".replace('Z', '+00:00')), fields={'Policy Key': 'refund-standard', 'Approval Limit USD': 250, 'Last Decided Case': 'case-approved'}),)))

setattr(update_records_stamped_standard_policy, '__module__', "dex_connectors.airtable.airtablemock")

def update_records_missing_policy_stamp() -> Case[UpdatedRecords]:
    return Case.branch_result(NOT_FOUND, UpdatedRecords(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateRecords", "Airtable found no such record."))

setattr(update_records_missing_policy_stamp, '__module__', "dex_connectors.airtable.airtablemock")

def update_records_policy_stamp_denied() -> Case[UpdatedRecords]:
    return Case.branch_result(PROVIDER_REJECTED, UpdatedRecords(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateRecords", "Airtable denied the policy update."))

setattr(update_records_policy_stamp_denied, '__module__', "dex_connectors.airtable.airtablemock")


class Mock:

    def __init__(self, connection_name: str = "airtable") -> None:
        self.list_records_script: Query[ListRecordsInput, RecordPage] = Query(LIST_RECORDS_DEFINITION)
        self.list_records_script.default(list_records_standard_refund_policy())
        self.get_record_script: Query[GetRecordInput, Record] = Query(GET_RECORD_DEFINITION)
        self.get_record_script.default(get_record_approved_decision_log())
        self.upsert_records_script: Mutation[UpsertRecordsInput, UpsertedRecords] = Mutation(UPSERT_RECORDS_DEFINITION)
        self.upsert_records_script.default(upsert_records_logged_approved_refund())
        self.update_records_script: Mutation[UpdateRecordsInput, UpdatedRecords] = Mutation(UPDATE_RECORDS_DEFINITION)
        self.update_records_script.default(update_records_stamped_standard_policy())
        self._connection = new_connection_with_operations(
            list_records=self.list_records_script,
            get_record=self.get_record_script,
            upsert_records=self.upsert_records_script,
            update_records=self.update_records_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.airtable.airtablemock")

__all__ = [
    "Mock",
    "list_records_listed",
    "list_records_offset_expired",
    "list_records_provider_rejected",
    "list_records_invalid_response",
    "list_records_defect",
    "list_records_retry",
    "list_records_standard_refund_policy",
    "list_records_expired_policy_offset",
    "list_records_policy_list_denied",
    "get_record_found",
    "get_record_not_found",
    "get_record_provider_rejected",
    "get_record_invalid_response",
    "get_record_defect",
    "get_record_retry",
    "get_record_approved_decision_log",
    "get_record_missing_decision_log",
    "get_record_decision_read_denied",
    "upsert_records_upserted",
    "upsert_records_provider_rejected",
    "upsert_records_invalid_response",
    "upsert_records_defect",
    "upsert_records_retry",
    "upsert_records_logged_approved_refund",
    "upsert_records_decision_upsert_denied",
    "update_records_updated",
    "update_records_not_found",
    "update_records_provider_rejected",
    "update_records_invalid_response",
    "update_records_defect",
    "update_records_retry",
    "update_records_stamped_standard_policy",
    "update_records_missing_policy_stamp",
    "update_records_policy_stamp_denied",
]
