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

CONNECTOR_ID = "wecom"
PROVIDER = "wecom"
SEND_GROUP_ROBOT_MESSAGE_OPERATION_ID = "sendGroupRobotMessage"
SEND_APPLICATION_MESSAGE_OPERATION_ID = "sendApplicationMessage"
DEFECT = "defect"
PROVIDER_REJECTED = "providerRejected"
SENT = "sent"
UNCERTAIN = "uncertain"

SEND_GROUP_ROBOT_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_GROUP_ROBOT_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "WeCom accepted the message for the group chat with errcode 0.", False),
        BranchDefinition(PROVIDER_REJECTED, "WeCom conclusively refused the post, such as a removed or disabled robot, an invalid key, or content over the size limit; nothing was posted.", True),
        BranchDefinition(UNCERTAIN, "The post may have reached WeCom, but the send cap was reached, WeCom answered and the response body broke, or a later post was refused or found an invalid connection; check the group chat before posting again.", True),
        BranchDefinition(DEFECT, "Local input, the connection method, the webhook key, or the dispatch fence is invalid; nothing was posted.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.LIMITED,
    max_sends=3,
)

SEND_APPLICATION_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_APPLICATION_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "WeCom accepted the message and returned its msgid; any recipients it could not reach are listed in the output.", False),
        BranchDefinition(PROVIDER_REJECTED, "WeCom conclusively refused the send or the access token request, such as an invalid secret or AgentId, an untrusted Worker IP, or no valid recipient; nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "The message may have been delivered, but the send cap was reached, WeCom answered and the response broke, a retry came 3 hours 50 minutes or more after the first attempt, past the safe part of WeCom's four-hour duplicate check, or a retry was refused or found invalid credentials; check with the recipients before sending again.", True),
        BranchDefinition(DEFECT, "Local input, the connection method, or the application credentials are invalid; nothing was sent.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.LIMITED,
    max_sends=3,
)
