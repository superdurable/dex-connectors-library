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

# The fixture adapter derives this spelling from the manifest operation ID.
UpsertRecordByExternalIdInput = UpsertRecordByExternalIDInput

__all__ = [
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
