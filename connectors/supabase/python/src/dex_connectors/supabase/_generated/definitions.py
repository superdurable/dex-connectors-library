# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "supabase"
PROVIDER = "supabase"
UPSERT_ROWS_OPERATION_ID = "upsertRows"
SELECT_ROWS_OPERATION_ID = "selectRows"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
SELECTED = "selected"
UPSERTED = "upserted"

UPSERT_ROWS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_ROWS_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "Upsert rows with a declared unique conflict key and merge duplicates.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SELECT_ROWS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SELECT_ROWS_OPERATION_ID),
    (
        BranchDefinition(SELECTED, "Select a bounded page of table rows through PostgREST.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
