# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import COQLValue, FindRecordsInput, FindRecordsOutput, GetRecordInput, ListModifiedRecordsInput, ListModifiedRecordsOutput, ListModuleFieldsInput, ListModuleFieldsOutput, ModuleField, PicklistValue, ProviderError, Record, RecordCondition, RecordSort, UpdateRecordInput, UpdateRecordOutput, UpsertRecordInput, UpsertRecordOutput
from .definitions import CONNECTOR_ID, PROVIDER, CONFLICT, DEFECT, FIND_RECORDS_DEFINITION, FOUND, GET_RECORD_DEFINITION, INVALID_RESPONSE, LISTED, LIST_MODIFIED_RECORDS_DEFINITION, LIST_MODULE_FIELDS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, RECORD_REJECTED, UPDATED, UPDATE_RECORD_DEFINITION, UPSERTED, UPSERT_RECORD_DEFINITION

def find_records_found(value: FindRecordsOutput, failure: Failure | None = None) -> Case[FindRecordsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_records_found, '__module__', "dex_connectors.zoho_crm.crmmock")

def find_records_not_found(value: FindRecordsOutput, failure: Failure | None = None) -> Case[FindRecordsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_records_not_found, '__module__', "dex_connectors.zoho_crm.crmmock")

def find_records_provider_rejected(value: FindRecordsOutput, failure: Failure | None = None) -> Case[FindRecordsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_records_provider_rejected, '__module__', "dex_connectors.zoho_crm.crmmock")

def find_records_invalid_response(value: FindRecordsOutput, failure: Failure | None = None) -> Case[FindRecordsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_records_invalid_response, '__module__', "dex_connectors.zoho_crm.crmmock")

def find_records_defect(value: FindRecordsOutput, failure: Failure | None = None) -> Case[FindRecordsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_records_defect, '__module__', "dex_connectors.zoho_crm.crmmock")

def find_records_retry(failure: Failure, after_seconds: float = 0) -> Case[FindRecordsOutput]:
    return Case.retry(failure, after_seconds)

setattr(find_records_retry, '__module__', "dex_connectors.zoho_crm.crmmock")

def find_records_acme_account() -> Case[FindRecordsOutput]:
    return Case.branch_result(FOUND, FindRecordsOutput(module='Accounts', records=(Record(module='Accounts', id='4150868000000376008', fields={'Account_Name': 'Acme Corp'}),)))

setattr(find_records_acme_account, '__module__', "dex_connectors.zoho_crm.crmmock")

def find_records_open_deal() -> Case[FindRecordsOutput]:
    return Case.branch_result(FOUND, FindRecordsOutput(module='Deals', records=(Record(module='Deals', id='4150868000000376022', fields={'Deal_Name': 'Renewal', 'Stage': 'Qualification', 'Modified_Time': '2026-10-25T12:00:00Z', 'Owner': {'id': '4150868000000376001'}}),)))

setattr(find_records_open_deal, '__module__', "dex_connectors.zoho_crm.crmmock")

def find_records_no_matching_records() -> Case[FindRecordsOutput]:
    return Case.branch_result(NOT_FOUND, FindRecordsOutput(module='Accounts', records=()))

setattr(find_records_no_matching_records, '__module__', "dex_connectors.zoho_crm.crmmock")

def find_records_access_denied() -> Case[FindRecordsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, FindRecordsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findRecords", "Zoho CRM denied the module operation."))

setattr(find_records_access_denied, '__module__', "dex_connectors.zoho_crm.crmmock")

def get_record_found(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_record_found, '__module__', "dex_connectors.zoho_crm.crmmock")

def get_record_not_found(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_record_not_found, '__module__', "dex_connectors.zoho_crm.crmmock")

def get_record_provider_rejected(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_record_provider_rejected, '__module__', "dex_connectors.zoho_crm.crmmock")

def get_record_invalid_response(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_record_invalid_response, '__module__', "dex_connectors.zoho_crm.crmmock")

def get_record_defect(value: Record, failure: Failure | None = None) -> Case[Record]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_record_defect, '__module__', "dex_connectors.zoho_crm.crmmock")

def get_record_retry(failure: Failure, after_seconds: float = 0) -> Case[Record]:
    return Case.retry(failure, after_seconds)

setattr(get_record_retry, '__module__', "dex_connectors.zoho_crm.crmmock")

def get_record_advanced_deal() -> Case[Record]:
    return Case.branch_result(FOUND, Record(module='Deals', id='4150868000000376022', fields={'Deal_Name': 'Renewal', 'Stage': 'Negotiation/Review', 'Owner': {'id': '4150868000000376001'}, 'Contact_Name': {'id': '4150868000000376011'}, 'Modified_Time': '2026-10-26T12:00:00Z'}))

setattr(get_record_advanced_deal, '__module__', "dex_connectors.zoho_crm.crmmock")

def get_record_record_missing() -> Case[Record]:
    return Case.branch_result(NOT_FOUND, Record(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getRecord", "Zoho CRM found no record with the ID."))

setattr(get_record_record_missing, '__module__', "dex_connectors.zoho_crm.crmmock")

def get_record_access_denied() -> Case[Record]:
    return Case.branch_result(PROVIDER_REJECTED, Record(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getRecord", "Zoho CRM denied the module operation."))

setattr(get_record_access_denied, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_modified_records_listed(value: ListModifiedRecordsOutput, failure: Failure | None = None) -> Case[ListModifiedRecordsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_modified_records_listed, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_modified_records_provider_rejected(value: ListModifiedRecordsOutput, failure: Failure | None = None) -> Case[ListModifiedRecordsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_modified_records_provider_rejected, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_modified_records_invalid_response(value: ListModifiedRecordsOutput, failure: Failure | None = None) -> Case[ListModifiedRecordsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_modified_records_invalid_response, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_modified_records_defect(value: ListModifiedRecordsOutput, failure: Failure | None = None) -> Case[ListModifiedRecordsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_modified_records_defect, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_modified_records_retry(failure: Failure, after_seconds: float = 0) -> Case[ListModifiedRecordsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_modified_records_retry, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_modified_records_changed_deals() -> Case[ListModifiedRecordsOutput]:
    return Case.branch_result(LISTED, ListModifiedRecordsOutput(module='Deals', records=(Record(module='Deals', id='4150868000000376022', fields={'Deal_Name': 'Renewal', 'Stage': 'Negotiation/Review', 'Modified_Time': '2026-10-26T12:00:00Z'}),), has_more=False, cursor='2026-10-26T12:00:00Z/4150868000000376022'))

setattr(list_modified_records_changed_deals, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_modified_records_access_denied() -> Case[ListModifiedRecordsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListModifiedRecordsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listModifiedRecords", "Zoho CRM denied the module operation."))

setattr(list_modified_records_access_denied, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_upserted(value: UpsertRecordOutput, failure: Failure | None = None) -> Case[UpsertRecordOutput]:
    return Case.branch_result(UPSERTED, value, failure)

setattr(upsert_record_upserted, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_conflict(value: UpsertRecordOutput, failure: Failure | None = None) -> Case[UpsertRecordOutput]:
    return Case.branch_result(CONFLICT, value, failure)

setattr(upsert_record_conflict, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_record_rejected(value: UpsertRecordOutput, failure: Failure | None = None) -> Case[UpsertRecordOutput]:
    return Case.branch_result(RECORD_REJECTED, value, failure)

setattr(upsert_record_record_rejected, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_provider_rejected(value: UpsertRecordOutput, failure: Failure | None = None) -> Case[UpsertRecordOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upsert_record_provider_rejected, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_invalid_response(value: UpsertRecordOutput, failure: Failure | None = None) -> Case[UpsertRecordOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upsert_record_invalid_response, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_defect(value: UpsertRecordOutput, failure: Failure | None = None) -> Case[UpsertRecordOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upsert_record_defect, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_retry(failure: Failure, after_seconds: float = 0) -> Case[UpsertRecordOutput]:
    return Case.retry(failure, after_seconds)

setattr(upsert_record_retry, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_acme_contact() -> Case[UpsertRecordOutput]:
    return Case.branch_result(UPSERTED, UpsertRecordOutput(module='Contacts', id='4150868000000376011', is_created=True, created_at='2026-10-26T11:00:00Z', modified_at='2026-10-26T11:00:00Z'))

setattr(upsert_record_acme_contact, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_account_upserted() -> Case[UpsertRecordOutput]:
    return Case.branch_result(UPSERTED, UpsertRecordOutput(module='Accounts', id='4150868000000376008', is_created=False, duplicate_field='Account_Name', modified_at='2026-10-26T11:00:00Z'))

setattr(upsert_record_account_upserted, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_duplicate_unique_field() -> Case[UpsertRecordOutput]:
    return Case.branch_result(CONFLICT, UpsertRecordOutput(module='Contacts', provider_errors=(ProviderError(code='DUPLICATE_DATA', field='Email', duplicate_record_id='4150868000000376099'),)), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "upsertRecord", "Zoho CRM found another record with a value of a unique field."))

setattr(upsert_record_duplicate_unique_field, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_invalid_contact_fields() -> Case[UpsertRecordOutput]:
    return Case.branch_result(RECORD_REJECTED, UpsertRecordOutput(module='Contacts', provider_errors=(ProviderError(code='MANDATORY_NOT_FOUND', field='Last_Name'),)), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "upsertRecord", "Zoho CRM rejected the contact fields."))

setattr(upsert_record_invalid_contact_fields, '__module__', "dex_connectors.zoho_crm.crmmock")

def upsert_record_access_denied() -> Case[UpsertRecordOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpsertRecordOutput(module='Contacts', provider_errors=(ProviderError(code='OAUTH_SCOPE_MISMATCH'),)), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "upsertRecord", "Zoho CRM denied the module operation."))

setattr(upsert_record_access_denied, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_updated(value: UpdateRecordOutput, failure: Failure | None = None) -> Case[UpdateRecordOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_record_updated, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_not_found(value: UpdateRecordOutput, failure: Failure | None = None) -> Case[UpdateRecordOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_record_not_found, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_conflict(value: UpdateRecordOutput, failure: Failure | None = None) -> Case[UpdateRecordOutput]:
    return Case.branch_result(CONFLICT, value, failure)

setattr(update_record_conflict, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_record_rejected(value: UpdateRecordOutput, failure: Failure | None = None) -> Case[UpdateRecordOutput]:
    return Case.branch_result(RECORD_REJECTED, value, failure)

setattr(update_record_record_rejected, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_provider_rejected(value: UpdateRecordOutput, failure: Failure | None = None) -> Case[UpdateRecordOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_record_provider_rejected, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_invalid_response(value: UpdateRecordOutput, failure: Failure | None = None) -> Case[UpdateRecordOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_record_invalid_response, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_defect(value: UpdateRecordOutput, failure: Failure | None = None) -> Case[UpdateRecordOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_record_defect, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateRecordOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_record_retry, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_advanced_renewal_deal() -> Case[UpdateRecordOutput]:
    return Case.branch_result(UPDATED, UpdateRecordOutput(module='Deals', id='4150868000000376022', modified_at='2026-10-26T12:00:00Z'))

setattr(update_record_advanced_renewal_deal, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_missing_record() -> Case[UpdateRecordOutput]:
    return Case.branch_result(NOT_FOUND, UpdateRecordOutput(module='Deals', provider_errors=(ProviderError(code='INVALID_DATA', field='id'),)), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateRecord", "Zoho CRM found no record with the ID."))

setattr(update_record_missing_record, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_duplicate_field() -> Case[UpdateRecordOutput]:
    return Case.branch_result(CONFLICT, UpdateRecordOutput(module='Deals', provider_errors=(ProviderError(code='DUPLICATE_DATA', field='External_Deal_ID', duplicate_record_id='4150868000000376099'),)), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "updateRecord", "Zoho CRM found another record with a value of a unique field."))

setattr(update_record_duplicate_field, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_invalid_fields() -> Case[UpdateRecordOutput]:
    return Case.branch_result(RECORD_REJECTED, UpdateRecordOutput(module='Deals', provider_errors=(ProviderError(code='INVALID_DATA', field='Stage'),)), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updateRecord", "Zoho CRM rejected the deal fields."))

setattr(update_record_invalid_fields, '__module__', "dex_connectors.zoho_crm.crmmock")

def update_record_access_denied() -> Case[UpdateRecordOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateRecordOutput(module='Deals', provider_errors=(ProviderError(code='OAUTH_SCOPE_MISMATCH'),)), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateRecord", "Zoho CRM denied the module operation."))

setattr(update_record_access_denied, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_module_fields_listed(value: ListModuleFieldsOutput, failure: Failure | None = None) -> Case[ListModuleFieldsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_module_fields_listed, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_module_fields_provider_rejected(value: ListModuleFieldsOutput, failure: Failure | None = None) -> Case[ListModuleFieldsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_module_fields_provider_rejected, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_module_fields_invalid_response(value: ListModuleFieldsOutput, failure: Failure | None = None) -> Case[ListModuleFieldsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_module_fields_invalid_response, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_module_fields_defect(value: ListModuleFieldsOutput, failure: Failure | None = None) -> Case[ListModuleFieldsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_module_fields_defect, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_module_fields_retry(failure: Failure, after_seconds: float = 0) -> Case[ListModuleFieldsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_module_fields_retry, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_module_fields_deal_fields() -> Case[ListModuleFieldsOutput]:
    return Case.branch_result(LISTED, ListModuleFieldsOutput(module='Deals', fields=(ModuleField(api_name='Stage', display_label='Stage', data_type='picklist', picklist_values=(PicklistValue(actual_value='Qualification', display_value='Qualification'), PicklistValue(actual_value='Negotiation/Review', display_value='Negotiation/Review'), PicklistValue(actual_value='Closed Won', display_value='Closed Won'))), ModuleField(api_name='Deal_Name', display_label='Deal Name', data_type='text'))))

setattr(list_module_fields_deal_fields, '__module__', "dex_connectors.zoho_crm.crmmock")

def list_module_fields_access_denied() -> Case[ListModuleFieldsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListModuleFieldsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listModuleFields", "Zoho CRM denied the module operation."))

setattr(list_module_fields_access_denied, '__module__', "dex_connectors.zoho_crm.crmmock")


class Mock:

    def __init__(self, connection_name: str = "zoho-crm") -> None:
        self.find_records_script: Query[FindRecordsInput, FindRecordsOutput] = Query(FIND_RECORDS_DEFINITION)
        self.find_records_script.default(find_records_acme_account())
        self.get_record_script: Query[GetRecordInput, Record] = Query(GET_RECORD_DEFINITION)
        self.get_record_script.default(get_record_advanced_deal())
        self.list_modified_records_script: Query[ListModifiedRecordsInput, ListModifiedRecordsOutput] = Query(LIST_MODIFIED_RECORDS_DEFINITION)
        self.list_modified_records_script.default(list_modified_records_changed_deals())
        self.upsert_record_script: Mutation[UpsertRecordInput, UpsertRecordOutput] = Mutation(UPSERT_RECORD_DEFINITION)
        self.upsert_record_script.default(upsert_record_acme_contact())
        self.update_record_script: Mutation[UpdateRecordInput, UpdateRecordOutput] = Mutation(UPDATE_RECORD_DEFINITION)
        self.update_record_script.default(update_record_advanced_renewal_deal())
        self.list_module_fields_script: Query[ListModuleFieldsInput, ListModuleFieldsOutput] = Query(LIST_MODULE_FIELDS_DEFINITION)
        self.list_module_fields_script.default(list_module_fields_deal_fields())
        self._connection = new_connection_with_operations(
            find_records=self.find_records_script,
            get_record=self.get_record_script,
            list_modified_records=self.list_modified_records_script,
            upsert_record=self.upsert_record_script,
            update_record=self.update_record_script,
            list_module_fields=self.list_module_fields_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zoho_crm.crmmock")

__all__ = [
    "Mock",
    "find_records_found",
    "find_records_not_found",
    "find_records_provider_rejected",
    "find_records_invalid_response",
    "find_records_defect",
    "find_records_retry",
    "find_records_acme_account",
    "find_records_open_deal",
    "find_records_no_matching_records",
    "find_records_access_denied",
    "get_record_found",
    "get_record_not_found",
    "get_record_provider_rejected",
    "get_record_invalid_response",
    "get_record_defect",
    "get_record_retry",
    "get_record_advanced_deal",
    "get_record_record_missing",
    "get_record_access_denied",
    "list_modified_records_listed",
    "list_modified_records_provider_rejected",
    "list_modified_records_invalid_response",
    "list_modified_records_defect",
    "list_modified_records_retry",
    "list_modified_records_changed_deals",
    "list_modified_records_access_denied",
    "upsert_record_upserted",
    "upsert_record_conflict",
    "upsert_record_record_rejected",
    "upsert_record_provider_rejected",
    "upsert_record_invalid_response",
    "upsert_record_defect",
    "upsert_record_retry",
    "upsert_record_acme_contact",
    "upsert_record_account_upserted",
    "upsert_record_duplicate_unique_field",
    "upsert_record_invalid_contact_fields",
    "upsert_record_access_denied",
    "update_record_updated",
    "update_record_not_found",
    "update_record_conflict",
    "update_record_record_rejected",
    "update_record_provider_rejected",
    "update_record_invalid_response",
    "update_record_defect",
    "update_record_retry",
    "update_record_advanced_renewal_deal",
    "update_record_missing_record",
    "update_record_duplicate_field",
    "update_record_invalid_fields",
    "update_record_access_denied",
    "list_module_fields_listed",
    "list_module_fields_provider_rejected",
    "list_module_fields_invalid_response",
    "list_module_fields_defect",
    "list_module_fields_retry",
    "list_module_fields_deal_fields",
    "list_module_fields_access_denied",
]
