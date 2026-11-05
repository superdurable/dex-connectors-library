# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetThreadReplyInput, GetThreadReplyOutput, ListThreadMessagesInput, ListThreadMessagesOutput, Message, PostChannelMessageInput, PostMessageOutput, PostThreadReplyInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_THREAD_REPLY_DEFINITION, INVALID_RESPONSE, LIST_THREAD_MESSAGES_DEFINITION, NOT_FOUND, POST_CHANNEL_MESSAGE_DEFINITION, POST_THREAD_REPLY_DEFINITION, PROVIDER_REJECTED, READ, SENT, UNCERTAIN

def list_thread_messages_read(value: ListThreadMessagesOutput, failure: Failure | None = None) -> Case[ListThreadMessagesOutput]:
    return Case.branch_result(READ, value, failure)

setattr(list_thread_messages_read, '__module__', "dex_connectors.slack.slackmock")

def list_thread_messages_provider_rejected(value: ListThreadMessagesOutput, failure: Failure | None = None) -> Case[ListThreadMessagesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_thread_messages_provider_rejected, '__module__', "dex_connectors.slack.slackmock")

def list_thread_messages_invalid_response(value: ListThreadMessagesOutput, failure: Failure | None = None) -> Case[ListThreadMessagesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_thread_messages_invalid_response, '__module__', "dex_connectors.slack.slackmock")

def list_thread_messages_defect(value: ListThreadMessagesOutput, failure: Failure | None = None) -> Case[ListThreadMessagesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_thread_messages_defect, '__module__', "dex_connectors.slack.slackmock")

def list_thread_messages_retry(failure: Failure, after_seconds: float = 0) -> Case[ListThreadMessagesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_thread_messages_retry, '__module__', "dex_connectors.slack.slackmock")

def list_thread_messages_approval_thread() -> Case[ListThreadMessagesOutput]:
    return Case.paged(READ, (ListThreadMessagesOutput(messages=(Message(channel_id='C0123456789', timestamp='1710000000.000100', user_id='U0123456789', text='Request approval for order 1042.'), Message(channel_id='C0123456789', timestamp='1710000001.000200', thread_timestamp='1710000000.000100', user_id='U0234567890', text='Can you confirm the delivery date?')), next_cursor='cursor-page-2'), ListThreadMessagesOutput(messages=(Message(channel_id='C0123456789', timestamp='1710000001.000200', thread_timestamp='1710000000.000100', user_id='U0234567890', text='Can you confirm the delivery date?'), Message(channel_id='C0123456789', timestamp='1710000002.000300', thread_timestamp='1710000000.000100', user_id='U0345678901', text='Delivery is scheduled for Thursday.')), next_cursor='cursor-page-3'), ListThreadMessagesOutput(messages=(Message(channel_id='C0123456789', timestamp='1710000003.000400', thread_timestamp='1710000000.000100', user_id='U0456789012', text='Approved for dispatch.'),))))

setattr(list_thread_messages_approval_thread, '__module__', "dex_connectors.slack.slackmock")

def list_thread_messages_read_rejected() -> Case[ListThreadMessagesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListThreadMessagesOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listThreadMessages", "Slack rejected the thread read because the app is not in the channel."))

setattr(list_thread_messages_read_rejected, '__module__', "dex_connectors.slack.slackmock")

def list_thread_messages_malformed_thread() -> Case[ListThreadMessagesOutput]:
    return Case.branch_result(INVALID_RESPONSE, ListThreadMessagesOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listThreadMessages", "Slack returned an invalid thread response."))

setattr(list_thread_messages_malformed_thread, '__module__', "dex_connectors.slack.slackmock")

def list_thread_messages_missing_thread_timestamp() -> Case[ListThreadMessagesOutput]:
    return Case.branch_result(DEFECT, ListThreadMessagesOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "listThreadMessages", "The thread timestamp is required."))

setattr(list_thread_messages_missing_thread_timestamp, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_found(value: GetThreadReplyOutput, failure: Failure | None = None) -> Case[GetThreadReplyOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_thread_reply_found, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_not_found(value: GetThreadReplyOutput, failure: Failure | None = None) -> Case[GetThreadReplyOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_thread_reply_not_found, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_provider_rejected(value: GetThreadReplyOutput, failure: Failure | None = None) -> Case[GetThreadReplyOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_thread_reply_provider_rejected, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_invalid_response(value: GetThreadReplyOutput, failure: Failure | None = None) -> Case[GetThreadReplyOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_thread_reply_invalid_response, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_defect(value: GetThreadReplyOutput, failure: Failure | None = None) -> Case[GetThreadReplyOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_thread_reply_defect, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_retry(failure: Failure, after_seconds: float = 0) -> Case[GetThreadReplyOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_thread_reply_retry, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_approval_reply() -> Case[GetThreadReplyOutput]:
    return Case.branch_result(FOUND, GetThreadReplyOutput(message=Message(channel_id='C0123456789', timestamp='1710000003.000400', thread_timestamp='1710000000.000100', user_id='U0456789012', text='Approved for dispatch.')))

setattr(get_thread_reply_approval_reply, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_reply_missing() -> Case[GetThreadReplyOutput]:
    return Case.branch_result(NOT_FOUND, GetThreadReplyOutput(message=Message()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getThreadReply", "The Slack thread reply does not exist."))

setattr(get_thread_reply_reply_missing, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_reply_rejected() -> Case[GetThreadReplyOutput]:
    return Case.branch_result(PROVIDER_REJECTED, GetThreadReplyOutput(message=Message()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getThreadReply", "Slack rejected the reply read because the app cannot access the channel."))

setattr(get_thread_reply_reply_rejected, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_malformed_reply() -> Case[GetThreadReplyOutput]:
    return Case.branch_result(INVALID_RESPONSE, GetThreadReplyOutput(message=Message()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getThreadReply", "Slack returned an invalid reply response."))

setattr(get_thread_reply_malformed_reply, '__module__', "dex_connectors.slack.slackmock")

def get_thread_reply_missing_reply_timestamp() -> Case[GetThreadReplyOutput]:
    return Case.branch_result(DEFECT, GetThreadReplyOutput(message=Message()), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "getThreadReply", "The reply timestamp is required."))

setattr(get_thread_reply_missing_reply_timestamp, '__module__', "dex_connectors.slack.slackmock")

def post_channel_message_sent(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, value, failure)

setattr(post_channel_message_sent, '__module__', "dex_connectors.slack.slackmock")

def post_channel_message_provider_rejected(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(post_channel_message_provider_rejected, '__module__', "dex_connectors.slack.slackmock")

def post_channel_message_uncertain(value: PostMessageOutput, failure: Failure) -> Case[PostMessageOutput]:
    return Case.uncertain(value, failure)

setattr(post_channel_message_uncertain, '__module__', "dex_connectors.slack.slackmock")

def post_channel_message_defect(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(post_channel_message_defect, '__module__', "dex_connectors.slack.slackmock")

def post_channel_message_retry(failure: Failure, after_seconds: float = 0) -> Case[PostMessageOutput]:
    return Case.retry(failure, after_seconds)

setattr(post_channel_message_retry, '__module__', "dex_connectors.slack.slackmock")

def post_channel_message_message_sent() -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, PostMessageOutput(message=Message(channel_id='C0123456789', timestamp='1710000010.000500', user_id='U0BOT123456', text='Order 1042 is ready for review.')))

setattr(post_channel_message_message_sent, '__module__', "dex_connectors.slack.slackmock")

def post_channel_message_channel_rejected() -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, PostMessageOutput(message=Message()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "postChannelMessage", "Slack rejected the channel post because the bot cannot write to the channel."))

setattr(post_channel_message_channel_rejected, '__module__', "dex_connectors.slack.slackmock")

def post_channel_message_post_outcome_unknown() -> Case[PostMessageOutput]:
    return Case.uncertain(PostMessageOutput(message=Message()), Failure(FailureKind.TRANSPORT, CONNECTOR_ID, "postChannelMessage", "The connection closed before Slack confirmed the channel post."))

setattr(post_channel_message_post_outcome_unknown, '__module__', "dex_connectors.slack.slackmock")

def post_channel_message_missing_message_text() -> Case[PostMessageOutput]:
    return Case.branch_result(DEFECT, PostMessageOutput(message=Message()), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "postChannelMessage", "The channel message text is required."))

setattr(post_channel_message_missing_message_text, '__module__', "dex_connectors.slack.slackmock")

def post_thread_reply_sent(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, value, failure)

setattr(post_thread_reply_sent, '__module__', "dex_connectors.slack.slackmock")

def post_thread_reply_provider_rejected(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(post_thread_reply_provider_rejected, '__module__', "dex_connectors.slack.slackmock")

def post_thread_reply_uncertain(value: PostMessageOutput, failure: Failure) -> Case[PostMessageOutput]:
    return Case.uncertain(value, failure)

setattr(post_thread_reply_uncertain, '__module__', "dex_connectors.slack.slackmock")

def post_thread_reply_defect(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(post_thread_reply_defect, '__module__', "dex_connectors.slack.slackmock")

def post_thread_reply_retry(failure: Failure, after_seconds: float = 0) -> Case[PostMessageOutput]:
    return Case.retry(failure, after_seconds)

setattr(post_thread_reply_retry, '__module__', "dex_connectors.slack.slackmock")

def post_thread_reply_reply_sent() -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, PostMessageOutput(message=Message(channel_id='C0123456789', timestamp='1710000011.000600', thread_timestamp='1710000000.000100', user_id='U0BOT123456', text='Processing complete.')))

setattr(post_thread_reply_reply_sent, '__module__', "dex_connectors.slack.slackmock")

def post_thread_reply_thread_rejected() -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, PostMessageOutput(message=Message()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "postThreadReply", "Slack could not find the channel thread for this reply."))

setattr(post_thread_reply_thread_rejected, '__module__', "dex_connectors.slack.slackmock")

def post_thread_reply_reply_outcome_unknown() -> Case[PostMessageOutput]:
    return Case.uncertain(PostMessageOutput(message=Message()), Failure(FailureKind.TRANSPORT, CONNECTOR_ID, "postThreadReply", "The connection closed before Slack confirmed the thread reply."))

setattr(post_thread_reply_reply_outcome_unknown, '__module__', "dex_connectors.slack.slackmock")

def post_thread_reply_missing_reply_text() -> Case[PostMessageOutput]:
    return Case.branch_result(DEFECT, PostMessageOutput(message=Message()), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "postThreadReply", "The thread reply text is required."))

setattr(post_thread_reply_missing_reply_text, '__module__', "dex_connectors.slack.slackmock")


class Mock:

    def __init__(self, connection_name: str = "slack") -> None:
        self.list_thread_messages_script: Query[ListThreadMessagesInput, ListThreadMessagesOutput] = Query(LIST_THREAD_MESSAGES_DEFINITION)
        self.list_thread_messages_script.default(list_thread_messages_approval_thread())
        self.get_thread_reply_script: Query[GetThreadReplyInput, GetThreadReplyOutput] = Query(GET_THREAD_REPLY_DEFINITION)
        self.get_thread_reply_script.default(get_thread_reply_approval_reply())
        self.post_channel_message_script: Mutation[PostChannelMessageInput, PostMessageOutput] = Mutation(POST_CHANNEL_MESSAGE_DEFINITION)
        self.post_channel_message_script.default(post_channel_message_message_sent())
        self.post_thread_reply_script: Mutation[PostThreadReplyInput, PostMessageOutput] = Mutation(POST_THREAD_REPLY_DEFINITION)
        self.post_thread_reply_script.default(post_thread_reply_reply_sent())
        self._connection = new_connection_with_operations(
            list_thread_messages=self.list_thread_messages_script,
            get_thread_reply=self.get_thread_reply_script,
            post_channel_message=self.post_channel_message_script,
            post_thread_reply=self.post_thread_reply_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.slack.slackmock")

__all__ = [
    "Mock",
    "list_thread_messages_read",
    "list_thread_messages_provider_rejected",
    "list_thread_messages_invalid_response",
    "list_thread_messages_defect",
    "list_thread_messages_retry",
    "list_thread_messages_approval_thread",
    "list_thread_messages_read_rejected",
    "list_thread_messages_malformed_thread",
    "list_thread_messages_missing_thread_timestamp",
    "get_thread_reply_found",
    "get_thread_reply_not_found",
    "get_thread_reply_provider_rejected",
    "get_thread_reply_invalid_response",
    "get_thread_reply_defect",
    "get_thread_reply_retry",
    "get_thread_reply_approval_reply",
    "get_thread_reply_reply_missing",
    "get_thread_reply_reply_rejected",
    "get_thread_reply_malformed_reply",
    "get_thread_reply_missing_reply_timestamp",
    "post_channel_message_sent",
    "post_channel_message_provider_rejected",
    "post_channel_message_uncertain",
    "post_channel_message_defect",
    "post_channel_message_retry",
    "post_channel_message_message_sent",
    "post_channel_message_channel_rejected",
    "post_channel_message_post_outcome_unknown",
    "post_channel_message_missing_message_text",
    "post_thread_reply_sent",
    "post_thread_reply_provider_rejected",
    "post_thread_reply_uncertain",
    "post_thread_reply_defect",
    "post_thread_reply_retry",
    "post_thread_reply_reply_sent",
    "post_thread_reply_thread_rejected",
    "post_thread_reply_reply_outcome_unknown",
    "post_thread_reply_missing_reply_text",
]
