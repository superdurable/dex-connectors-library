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

CONNECTOR_ID = "jihugitlab"
PROVIDER = "jihugitlab"
LIST_MEMBER_PROJECTS_OPERATION_ID = "listMemberProjects"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_MEMBER_PROJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MEMBER_PROJECTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "JiHu GitLab returned member projects.", False),
        BranchDefinition(PROVIDER_REJECTED, "JiHu GitLab rejected the token or access.", True),
        BranchDefinition(INVALID_RESPONSE, "JiHu GitLab returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or pagination are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
