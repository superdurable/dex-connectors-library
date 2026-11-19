# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RepeatOnUnknown,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "mysql"
PROVIDER = "mysql"
QUERY_OPERATION_ID = "query"
EXECUTE_OPERATION_ID = "execute"
EXECUTE_NON_IDEMPOTENT_OPERATION_ID = "executeNonIdempotent"
COMPLETED = "completed"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LIMIT_EXCEEDED = "limitExceeded"
PROVIDER_REJECTED = "providerRejected"
TRUNCATED = "truncated"
UNCERTAIN = "uncertain"

QUERY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_OPERATION_ID),
    (
        BranchDefinition(COMPLETED, "The server ran the read-only statement and returned every row, possibly none, within maxRows and maxResponseBytes.", False),
        BranchDefinition(TRUNCATED, "The statement produced more rows or bytes than maxRows or maxResponseBytes allow; the Result holds only the rows that fit.", True),
        BranchDefinition(PROVIDER_REJECTED, "The server conclusively refused the connection or statement, such as bad credentials, a syntax error, a missing table, a permission error, a write inside the read-only transaction, or statementTimeout.", True),
        BranchDefinition(INVALID_RESPONSE, "The server returned a value the connector cannot decode under its documented type mapping.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

EXECUTE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, EXECUTE_OPERATION_ID),
    (
        BranchDefinition(COMPLETED, "The server committed the statement's transaction; the Result holds the rows affected, the last insert ID, the warning count, and any RETURNING rows.", False),
        BranchDefinition(LIMIT_EXCEEDED, "The statement affected more rows than maxRowsAffected, or its RETURNING rows exceeded maxRows or maxResponseBytes, so the connector rolled back and nothing was written.", True),
        BranchDefinition(PROVIDER_REJECTED, "The server conclusively refused the connection, statement, or commit, such as a duplicate key, a syntax error, or a permission error; nothing was written.", True),
        BranchDefinition(UNCERTAIN, "Retained for identity stability and never selected: a COMMIT with an unknown outcome is retried, because execute statements must be repeat-safe.", True),
        BranchDefinition(INVALID_RESPONSE, "The server returned a value the connector cannot decode under its documented type mapping; the connector rolled back and nothing was written.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.SAFE,
)

EXECUTE_NON_IDEMPOTENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, EXECUTE_NON_IDEMPOTENT_OPERATION_ID),
    (
        BranchDefinition(COMPLETED, "The server committed the statement's transaction; the Result holds the rows affected, the last insert ID, the warning count, and any RETURNING rows.", False),
        BranchDefinition(LIMIT_EXCEEDED, "The statement affected more rows than maxRowsAffected, or its RETURNING rows exceeded maxRows or maxResponseBytes, so the connector rolled back and nothing was written.", True),
        BranchDefinition(PROVIDER_REJECTED, "The server conclusively refused the connection, statement, or commit, such as a duplicate key, a syntax error, or a permission error; nothing was written.", True),
        BranchDefinition(UNCERTAIN, "COMMIT may have reached the server, in this or an earlier attempt, and its outcome is unknown; the connector never runs the statement again, so inspect the database before writing again.", True),
        BranchDefinition(INVALID_RESPONSE, "The server returned a value the connector cannot decode under its documented type mapping; the connector rolled back and nothing was written.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.UNSAFE,
)
