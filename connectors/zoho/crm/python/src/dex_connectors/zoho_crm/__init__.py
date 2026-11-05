# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoho CRM connector operations and generated Dex bindings."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_project_connection
from .models import (
    COQLValue,
    FindRecordsInput,
    FindRecordsOutput,
    GetRecordInput,
    ListModifiedRecordsInput,
    ListModifiedRecordsOutput,
    ListModuleFieldsInput,
    ListModuleFieldsOutput,
    ModuleField,
    PicklistValue,
    ProviderError,
    Record,
    RecordCondition,
    RecordSort,
    UpdateRecordInput,
    UpdateRecordOutput,
    UpsertRecordInput,
    UpsertRecordOutput,
)
from .operations import (
    FindRecordsOperation,
    GetRecordOperation,
    ListModifiedRecordsOperation,
    ListModuleFieldsOperation,
    UpdateRecordOperation,
    UpsertRecordOperation,
)
from .steps import (
    new_find_records_step,
    new_get_record_step,
    new_list_modified_records_step,
    new_list_module_fields_step,
    new_update_record_step,
    new_upsert_record_step,
)

__all__ = [
    "COQLValue", "Client", "Config", "Connection", "Credentials", "FindRecordsInput",
    "FindRecordsOperation", "FindRecordsOutput", "GetRecordInput", "GetRecordOperation",
    "ListModifiedRecordsInput", "ListModifiedRecordsOperation", "ListModifiedRecordsOutput",
    "ListModuleFieldsInput", "ListModuleFieldsOperation", "ListModuleFieldsOutput", "ModuleField",
    "PicklistValue", "ProviderError", "Record", "RecordCondition", "RecordSort", "UpdateRecordInput",
    "UpdateRecordOperation", "UpdateRecordOutput", "UpsertRecordInput", "UpsertRecordOperation",
    "UpsertRecordOutput", "new_connection", "new_find_records_step", "new_get_record_step",
    "new_list_modified_records_step", "new_list_module_fields_step", "new_project_connection",
    "new_update_record_step", "new_upsert_record_step",
]
