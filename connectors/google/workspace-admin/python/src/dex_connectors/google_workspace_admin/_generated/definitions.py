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

CONNECTOR_ID = "google-workspace-admin"
PROVIDER = "google"
GET_USER_OPERATION_ID = "getUser"
LIST_USERS_OPERATION_ID = "listUsers"
CREATE_USER_OPERATION_ID = "createUser"
SUSPEND_USER_OPERATION_ID = "suspendUser"
UNSUSPEND_USER_OPERATION_ID = "unsuspendUser"
ADD_USER_TO_GROUP_OPERATION_ID = "addUserToGroup"
REMOVE_USER_FROM_GROUP_OPERATION_ID = "removeUserFromGroup"
ADDED = "added"
ALREADY_EXISTS = "alreadyExists"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
REMOVED = "removed"
SUSPENDED = "suspended"
UNSUSPENDED = "unsuspended"

GET_USER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_USER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Google returned the account.", False),
        BranchDefinition(NOT_FOUND, "No account has this address or ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the read, such as an administrator without user read privileges.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized account.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_USERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_USERS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google returned one page of accounts, which may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the listing, such as an unknown domain, an invalid query, or an expired page token.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_USER_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_USER_OPERATION_ID),
    (
        BranchDefinition(CREATED, "The account exists and carries this Step's creation key.", False),
        BranchDefinition(ALREADY_EXISTS, "The address belongs to another account, alias, or group that this Step did not create.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the account, such as an unknown organizational unit or an unverified domain.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized account while reading back an existing address.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SUSPEND_USER_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SUSPEND_USER_OPERATION_ID),
    (
        BranchDefinition(SUSPENDED, "Google reports the account as suspended.", False),
        BranchDefinition(NOT_FOUND, "No account has this address or ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the change, such as a User Management Admin suspending an administrator.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized account.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UNSUSPEND_USER_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UNSUSPEND_USER_OPERATION_ID),
    (
        BranchDefinition(UNSUSPENDED, "Google reports the account as not suspended.", False),
        BranchDefinition(NOT_FOUND, "No account has this address or ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the change or kept the account suspended, such as one Google suspended for abuse.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized account.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_USER_TO_GROUP_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_USER_TO_GROUP_OPERATION_ID),
    (
        BranchDefinition(ADDED, "The account is a direct member of the group.", False),
        BranchDefinition(NOT_FOUND, "The group or the account does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the membership, such as a disabled MANAGER role or missing group privileges.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized membership.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

REMOVE_USER_FROM_GROUP_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, REMOVE_USER_FROM_GROUP_OPERATION_ID),
    (
        BranchDefinition(REMOVED, "The account is not a direct member of the group.", False),
        BranchDefinition(NOT_FOUND, "The group does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the removal or the group check.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized group check.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
