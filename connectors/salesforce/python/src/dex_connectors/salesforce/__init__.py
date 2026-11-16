# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Salesforce connector for Dex Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_project_connection
from .models import (
    GetRecordInput,
    ProviderError,
    QueryRecordsInput,
    QueryRecordsOutput,
    Record,
    RecordModifiedEvent,
    RecordModifiedTriggerConfiguration,
    SOQLValue,
    UpdateRecordInput,
    UpdateRecordOutput,
    UpsertRecordByExternalIDInput,
    UpsertRecordByExternalIDOutput,
)
from .operations import (
    GetRecordOperation,
    QueryRecordsOperation,
    UpdateRecordOperation,
    UpsertRecordByExternalIDOperation,
)
from .steps import (
    new_get_record_step,
    new_query_records_step,
    new_update_record_step,
    new_upsert_record_by_external_id_step,
)
from .triggers import (
    MAX_TRIGGER_FIELD_NAMES,
    RECORD_CHANGE_CREATED,
    RECORD_CHANGE_UPDATED,
    RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION,
    UI_FIELD_NAMES_INPUT_PORT_FIELD_NAMES,
    UI_S_OBJECT_PICKER_PORT_S_OBJECT_TYPE,
    UI_UNIT_FIELD_NAMES_INPUT,
    UI_UNIT_S_OBJECT_PICKER,
    ProjectRecordModifiedPolledTriggerRoute,
    ProjectRecordTriggerRunner,
    RecordModifiedPolledTriggerRoute,
    RecordPollError,
    decode_record_modified_event_payload,
    decode_record_modified_polled_trigger_configuration,
    define_record_modified_polled_trigger_binding,
    new_project_record_trigger_runner,
    new_record_modified_polled_trigger,
    new_record_trigger_runner,
    validate_record_modified_trigger_configuration,
)

# The fixture adapter derives this spelling from the manifest operation ID.
UpsertRecordByExternalIdInput = UpsertRecordByExternalIDInput

__all__ = [
    "MAX_TRIGGER_FIELD_NAMES",
    "RECORD_CHANGE_CREATED",
    "RECORD_CHANGE_UPDATED",
    "RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION",
    "UI_FIELD_NAMES_INPUT_PORT_FIELD_NAMES",
    "UI_S_OBJECT_PICKER_PORT_S_OBJECT_TYPE",
    "UI_UNIT_FIELD_NAMES_INPUT",
    "UI_UNIT_S_OBJECT_PICKER",
    "ProjectRecordModifiedPolledTriggerRoute",
    "ProjectRecordTriggerRunner",
    "RecordModifiedEvent",
    "RecordModifiedPolledTriggerRoute",
    "RecordModifiedTriggerConfiguration",
    "RecordPollError",
    "decode_record_modified_event_payload",
    "decode_record_modified_polled_trigger_configuration",
    "define_record_modified_polled_trigger_binding",
    "new_project_record_trigger_runner",
    "new_record_modified_polled_trigger",
    "new_record_trigger_runner",
    "validate_record_modified_trigger_configuration",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetRecordInput",
    "GetRecordOperation",
    "ProviderError",
    "QueryRecordsInput",
    "QueryRecordsOperation",
    "QueryRecordsOutput",
    "Record",
    "SOQLValue",
    "UpdateRecordInput",
    "UpdateRecordOperation",
    "UpdateRecordOutput",
    "UpsertRecordByExternalIDInput",
    "UpsertRecordByExternalIDOperation",
    "UpsertRecordByExternalIDOutput",
    "new_connection",
    "new_get_record_step",
    "new_project_connection",
    "new_query_records_step",
    "new_update_record_step",
    "new_upsert_record_by_external_id_step",
]
