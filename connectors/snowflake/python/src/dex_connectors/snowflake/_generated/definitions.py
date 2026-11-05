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

CONNECTOR_ID = "snowflake"
PROVIDER = "snowflake"
SUBMIT_STATEMENT_OPERATION_ID = "submitStatement"
GET_STATEMENT_RESULT_OPERATION_ID = "getStatementResult"
CANCEL_STATEMENT_OPERATION_ID = "cancelStatement"
CANCELED = "canceled"
COMPLETED = "completed"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
RUNNING = "running"
SUBMITTED = "submitted"
TRUNCATED = "truncated"

SUBMIT_STATEMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SUBMIT_STATEMENT_OPERATION_ID),
    (
        BranchDefinition(SUBMITTED, "Snowflake accepted the statement and returned its handle; it may still be running. Read its status and rows with getStatementResult after a durable Timer.", False),
        BranchDefinition(PROVIDER_REJECTED, "Snowflake conclusively refused the request or the statement, such as bad credentials, a missing warehouse or privilege, a SQL compilation error, or a binding mismatch; nothing ran.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, credentials, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_STATEMENT_RESULT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_STATEMENT_RESULT_OPERATION_ID),
    (
        BranchDefinition(COMPLETED, "The statement finished successfully; the Result holds its columns, partition count, total row count, and every row of the requested partition, possibly none, within maxRows and maxResponseBytes.", False),
        BranchDefinition(RUNNING, "The statement is still queued or running; wait on a durable Timer and read it again.", True),
        BranchDefinition(TRUNCATED, "The statement finished, but the requested partition holds more rows or bytes than maxRows or maxResponseBytes allow; the Result holds only the rows that fit.", True),
        BranchDefinition(PROVIDER_REJECTED, "The statement failed, was canceled, or exceeded statementTimeoutSeconds, or Snowflake refused the request, such as bad credentials or an unknown statement handle; the Receipt carries Snowflake's code and SQLSTATE.", True),
        BranchDefinition(INVALID_RESPONSE, "Snowflake returned a response or value the connector cannot decode under its documented type mapping.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, credentials, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CANCEL_STATEMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CANCEL_STATEMENT_OPERATION_ID),
    (
        BranchDefinition(CANCELED, "Snowflake accepted the cancellation; reading the statement afterwards reports it as canceled unless it had already finished.", False),
        BranchDefinition(PROVIDER_REJECTED, "Snowflake refused the cancellation, such as an unknown statement handle, a statement that already finished, or bad credentials; the Receipt carries Snowflake's code and SQLSTATE.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, credentials, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
