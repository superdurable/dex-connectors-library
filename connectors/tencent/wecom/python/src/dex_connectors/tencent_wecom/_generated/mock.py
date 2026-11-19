# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation

from ..connection import Connection, new_connection_with_operations
from ..models import SendApplicationMessageInput, SendGroupRobotMessageInput, SentApplicationMessage, SentGroupRobotMessage
from .definitions import CONNECTOR_ID, DEFECT, PROVIDER_REJECTED, SEND_APPLICATION_MESSAGE_DEFINITION, SEND_GROUP_ROBOT_MESSAGE_DEFINITION, SENT, UNCERTAIN

def send_group_robot_message_sent(value: SentGroupRobotMessage, failure: Failure | None = None) -> Case[SentGroupRobotMessage]:
    return Case.branch_result(SENT, value, failure)

setattr(send_group_robot_message_sent, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_group_robot_message_provider_rejected(value: SentGroupRobotMessage, failure: Failure | None = None) -> Case[SentGroupRobotMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_group_robot_message_provider_rejected, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_group_robot_message_uncertain(value: SentGroupRobotMessage, failure: Failure) -> Case[SentGroupRobotMessage]:
    return Case.uncertain(value, failure)

setattr(send_group_robot_message_uncertain, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_group_robot_message_defect(value: SentGroupRobotMessage, failure: Failure | None = None) -> Case[SentGroupRobotMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_group_robot_message_defect, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_group_robot_message_retry(failure: Failure, after_seconds: float = 0) -> Case[SentGroupRobotMessage]:
    return Case.retry(failure, after_seconds)

setattr(send_group_robot_message_retry, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_group_robot_message_markdown_posted() -> Case[SentGroupRobotMessage]:
    return Case.branch_result(SENT, SentGroupRobotMessage(message_type='markdown', content_bytes=96))

setattr(send_group_robot_message_markdown_posted, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_application_message_sent(value: SentApplicationMessage, failure: Failure | None = None) -> Case[SentApplicationMessage]:
    return Case.branch_result(SENT, value, failure)

setattr(send_application_message_sent, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_application_message_provider_rejected(value: SentApplicationMessage, failure: Failure | None = None) -> Case[SentApplicationMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_application_message_provider_rejected, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_application_message_uncertain(value: SentApplicationMessage, failure: Failure) -> Case[SentApplicationMessage]:
    return Case.uncertain(value, failure)

setattr(send_application_message_uncertain, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_application_message_defect(value: SentApplicationMessage, failure: Failure | None = None) -> Case[SentApplicationMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_application_message_defect, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_application_message_retry(failure: Failure, after_seconds: float = 0) -> Case[SentApplicationMessage]:
    return Case.retry(failure, after_seconds)

setattr(send_application_message_retry, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

def send_application_message_text_delivered() -> Case[SentApplicationMessage]:
    return Case.branch_result(SENT, SentApplicationMessage(message_id='mock-msgid-6pD0aQe3s4k', invalid_user_ids=(), invalid_department_ids=(), invalid_tag_ids=(), unlicensed_user_ids=()))

setattr(send_application_message_text_delivered, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")


class Mock:

    def __init__(self, connection_name: str = "wecom") -> None:
        self.send_group_robot_message_script: Mutation[SendGroupRobotMessageInput, SentGroupRobotMessage] = Mutation(SEND_GROUP_ROBOT_MESSAGE_DEFINITION)
        self.send_group_robot_message_script.default(send_group_robot_message_markdown_posted())
        self.send_application_message_script: Mutation[SendApplicationMessageInput, SentApplicationMessage] = Mutation(SEND_APPLICATION_MESSAGE_DEFINITION)
        self.send_application_message_script.default(send_application_message_text_delivered())
        self._connection = new_connection_with_operations(
            send_group_robot_message=self.send_group_robot_message_script,
            send_application_message=self.send_application_message_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.tencent_wecom.tencent_wecommock")

__all__ = [
    "Mock",
    "send_group_robot_message_sent",
    "send_group_robot_message_provider_rejected",
    "send_group_robot_message_uncertain",
    "send_group_robot_message_defect",
    "send_group_robot_message_retry",
    "send_group_robot_message_markdown_posted",
    "send_application_message_sent",
    "send_application_message_provider_rejected",
    "send_application_message_uncertain",
    "send_application_message_defect",
    "send_application_message_retry",
    "send_application_message_text_delivered",
]
