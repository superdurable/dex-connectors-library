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

CONNECTOR_ID = "microsoft-entra-id"
PROVIDER = "microsoft"
GET_USER_OPERATION_ID = "getUser"
LIST_USERS_OPERATION_ID = "listUsers"
CREATE_USER_OPERATION_ID = "createUser"
DISABLE_USER_OPERATION_ID = "disableUser"
ENABLE_USER_OPERATION_ID = "enableUser"
REVOKE_SIGN_IN_SESSIONS_OPERATION_ID = "revokeSignInSessions"
ADD_USER_TO_GROUP_OPERATION_ID = "addUserToGroup"
REMOVE_USER_FROM_GROUP_OPERATION_ID = "removeUserFromGroup"
ADDED = "added"
ALREADY_EXISTS = "alreadyExists"
CREATED = "created"
DEFECT = "defect"
DISABLED = "disabled"
ENABLED = "enabled"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
REMOVED = "removed"
REVOKED = "revoked"

GET_USER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_USER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Microsoft Graph returned the account.", False),
        BranchDefinition(NOT_FOUND, "No account has this object ID or user principal name.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the read, such as a token without User.Read.All.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized account.", True),
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
        BranchDefinition(LISTED, "Microsoft Graph returned one page of accounts, which may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the listing, such as an unsupported filter.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized page.", True),
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
        BranchDefinition(ALREADY_EXISTS, "The user principal name belongs to another account, or to a deleted one, that this Step did not create.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the account, such as an unverified or federated domain or a token without User.Create.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized account while reading back an existing user principal name.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

DISABLE_USER_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, DISABLE_USER_OPERATION_ID),
    (
        BranchDefinition(DISABLED, "Microsoft Graph reports the account as disabled.", False),
        BranchDefinition(NOT_FOUND, "No account has this object ID or user principal name.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the change, such as an account synchronized from on-premises Active Directory or one with an administrator role the caller cannot manage.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized account.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ENABLE_USER_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ENABLE_USER_OPERATION_ID),
    (
        BranchDefinition(ENABLED, "Microsoft Graph reports the account as enabled.", False),
        BranchDefinition(NOT_FOUND, "No account has this object ID or user principal name.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the change, such as an account synchronized from on-premises Active Directory or one with an administrator role the caller cannot manage.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized account.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

REVOKE_SIGN_IN_SESSIONS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, REVOKE_SIGN_IN_SESSIONS_OPERATION_ID),
    (
        BranchDefinition(REVOKED, "Microsoft Graph accepted the revocation; Microsoft documents that tokens can keep working for a few minutes.", False),
        BranchDefinition(NOT_FOUND, "No account has this object ID or user principal name.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the revocation, such as a token without User.RevokeSessions.All.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized revocation response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

ADD_USER_TO_GROUP_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_USER_TO_GROUP_OPERATION_ID),
    (
        BranchDefinition(ADDED, "The account is a direct member of the group.", False),
        BranchDefinition(NOT_FOUND, "The group or the account does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the membership, such as a dynamic, distribution, mail-enabled security, or role-assignable group.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized membership check.", True),
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
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Graph conclusively rejected the removal, such as a group with dynamic membership.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Graph returned an invalid or oversized group check.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
