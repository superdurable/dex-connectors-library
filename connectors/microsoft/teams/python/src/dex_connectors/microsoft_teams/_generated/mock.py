# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListThreadRepliesInput, ListThreadRepliesOutput, Message, MessageSender, PostChannelMessageInput, PostChatMessageInput, PostMessageOutput, PostThreadReplyInput
from .definitions import CONNECTOR_ID, PROVIDER, DEFECT, INVALID_RESPONSE, LIST_THREAD_REPLIES_DEFINITION, NOT_FOUND, POST_CHANNEL_MESSAGE_DEFINITION, POST_CHAT_MESSAGE_DEFINITION, POST_THREAD_REPLY_DEFINITION, PROVIDER_REJECTED, READ, SENT, UNCERTAIN

def list_thread_replies_read(value: ListThreadRepliesOutput, failure: Failure | None = None) -> Case[ListThreadRepliesOutput]:
    return Case.branch_result(READ, value, failure)

setattr(list_thread_replies_read, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def list_thread_replies_not_found(value: ListThreadRepliesOutput, failure: Failure | None = None) -> Case[ListThreadRepliesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_thread_replies_not_found, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def list_thread_replies_provider_rejected(value: ListThreadRepliesOutput, failure: Failure | None = None) -> Case[ListThreadRepliesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_thread_replies_provider_rejected, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def list_thread_replies_invalid_response(value: ListThreadRepliesOutput, failure: Failure | None = None) -> Case[ListThreadRepliesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_thread_replies_invalid_response, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def list_thread_replies_defect(value: ListThreadRepliesOutput, failure: Failure | None = None) -> Case[ListThreadRepliesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_thread_replies_defect, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def list_thread_replies_retry(failure: Failure, after_seconds: float = 0) -> Case[ListThreadRepliesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_thread_replies_retry, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def list_thread_replies_incident_acknowledgement() -> Case[ListThreadRepliesOutput]:
    return Case.branch_result(READ, ListThreadRepliesOutput(replies=(Message(id='1616990032036', reply_to_id='1616990032035', message_type='message', created_at='2026-10-01T09:31:00Z', sender=MessageSender(user_id='8ea0e38b-efb3-4757-924a-5f94061cf8c2', display_name='Incident Bot'), importance='normal', content_type='text', text='Status: SEV2 incident INC-1042 is being investigated. Reply ack in this thread to acknowledge.', web_url='https://teams.microsoft.com/l/message/19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2/1616990032036'), Message(id='1616990032040', reply_to_id='1616990032035', message_type='message', created_at='2026-10-01T09:32:00Z', sender=MessageSender(user_id='5f1c2d3e-4b5a-4c6d-8e7f-901a2b3c4d5e', display_name='Ada Lovelace'), importance='normal', content_type='text', text='ACK INC-1042', web_url='https://teams.microsoft.com/l/message/19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2/1616990032040'))))

setattr(list_thread_replies_incident_acknowledgement, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def list_thread_replies_missing_thread() -> Case[ListThreadRepliesOutput]:
    return Case.branch_result(NOT_FOUND, ListThreadRepliesOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listThreadReplies", "The thread does not exist."))

setattr(list_thread_replies_missing_thread, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def list_thread_replies_read_denied() -> Case[ListThreadRepliesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListThreadRepliesOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listThreadReplies", "Microsoft Graph denied the thread read."))

setattr(list_thread_replies_read_denied, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def list_thread_replies_invalid_page() -> Case[ListThreadRepliesOutput]:
    return Case.branch_result(INVALID_RESPONSE, ListThreadRepliesOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listThreadReplies", "The reply page is invalid."))

setattr(list_thread_replies_invalid_page, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_channel_message_sent(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, value, failure)

setattr(post_channel_message_sent, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_channel_message_provider_rejected(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(post_channel_message_provider_rejected, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_channel_message_uncertain(value: PostMessageOutput, failure: Failure) -> Case[PostMessageOutput]:
    return Case.uncertain(value, failure)

setattr(post_channel_message_uncertain, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_channel_message_defect(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(post_channel_message_defect, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_channel_message_retry(failure: Failure, after_seconds: float = 0) -> Case[PostMessageOutput]:
    return Case.retry(failure, after_seconds)

setattr(post_channel_message_retry, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_channel_message_incident_posted() -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, PostMessageOutput(message_id='1616990032035', team_id='fbe2bf47-16c8-47cf-b4a5-4b9b187c508b', channel_id='19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2', created_at='2026-10-01T09:30:00Z', sender=MessageSender(user_id='8ea0e38b-efb3-4757-924a-5f94061cf8c2', display_name='Incident Bot'), web_url='https://teams.microsoft.com/l/message/19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2/1616990032035'))

setattr(post_channel_message_incident_posted, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_channel_message_channel_post_denied() -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, PostMessageOutput(sender=MessageSender()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "postChannelMessage", "Microsoft Graph denied the channel post."))

setattr(post_channel_message_channel_post_denied, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_thread_reply_sent(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, value, failure)

setattr(post_thread_reply_sent, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_thread_reply_provider_rejected(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(post_thread_reply_provider_rejected, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_thread_reply_uncertain(value: PostMessageOutput, failure: Failure) -> Case[PostMessageOutput]:
    return Case.uncertain(value, failure)

setattr(post_thread_reply_uncertain, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_thread_reply_defect(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(post_thread_reply_defect, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_thread_reply_retry(failure: Failure, after_seconds: float = 0) -> Case[PostMessageOutput]:
    return Case.retry(failure, after_seconds)

setattr(post_thread_reply_retry, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_thread_reply_incident_status_reply() -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, PostMessageOutput(message_id='1616990032036', team_id='fbe2bf47-16c8-47cf-b4a5-4b9b187c508b', channel_id='19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2', reply_to_id='1616990032035', created_at='2026-10-01T09:31:00Z', sender=MessageSender(user_id='8ea0e38b-efb3-4757-924a-5f94061cf8c2', display_name='Incident Bot'), web_url='https://teams.microsoft.com/l/message/19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2/1616990032036'))

setattr(post_thread_reply_incident_status_reply, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_thread_reply_reply_denied() -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, PostMessageOutput(sender=MessageSender()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "postThreadReply", "Microsoft Graph denied the thread reply."))

setattr(post_thread_reply_reply_denied, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_chat_message_sent(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, value, failure)

setattr(post_chat_message_sent, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_chat_message_provider_rejected(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(post_chat_message_provider_rejected, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_chat_message_uncertain(value: PostMessageOutput, failure: Failure) -> Case[PostMessageOutput]:
    return Case.uncertain(value, failure)

setattr(post_chat_message_uncertain, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_chat_message_defect(value: PostMessageOutput, failure: Failure | None = None) -> Case[PostMessageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(post_chat_message_defect, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_chat_message_retry(failure: Failure, after_seconds: float = 0) -> Case[PostMessageOutput]:
    return Case.retry(failure, after_seconds)

setattr(post_chat_message_retry, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_chat_message_incident_escalation() -> Case[PostMessageOutput]:
    return Case.branch_result(SENT, PostMessageOutput(message_id='1616990032050', chat_id='19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2', created_at='2026-10-01T09:40:00Z', sender=MessageSender(user_id='8ea0e38b-efb3-4757-924a-5f94061cf8c2', display_name='Incident Bot'), web_url='https://teams.microsoft.com/l/message/19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2/1616990032050'))

setattr(post_chat_message_incident_escalation, '__module__', "dex_connectors.microsoft_teams.teamsmock")

def post_chat_message_chat_post_denied() -> Case[PostMessageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, PostMessageOutput(sender=MessageSender()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "postChatMessage", "Microsoft Graph denied the chat post."))

setattr(post_chat_message_chat_post_denied, '__module__', "dex_connectors.microsoft_teams.teamsmock")


class Mock:

    def __init__(self, connection_name: str = "microsoft-teams") -> None:
        self.list_thread_replies_script: Query[ListThreadRepliesInput, ListThreadRepliesOutput] = Query(LIST_THREAD_REPLIES_DEFINITION)
        self.list_thread_replies_script.default(list_thread_replies_incident_acknowledgement())
        self.post_channel_message_script: Mutation[PostChannelMessageInput, PostMessageOutput] = Mutation(POST_CHANNEL_MESSAGE_DEFINITION)
        self.post_channel_message_script.default(post_channel_message_incident_posted())
        self.post_thread_reply_script: Mutation[PostThreadReplyInput, PostMessageOutput] = Mutation(POST_THREAD_REPLY_DEFINITION)
        self.post_thread_reply_script.default(post_thread_reply_incident_status_reply())
        self.post_chat_message_script: Mutation[PostChatMessageInput, PostMessageOutput] = Mutation(POST_CHAT_MESSAGE_DEFINITION)
        self.post_chat_message_script.default(post_chat_message_incident_escalation())
        self._connection = new_connection_with_operations(
            list_thread_replies=self.list_thread_replies_script,
            post_channel_message=self.post_channel_message_script,
            post_thread_reply=self.post_thread_reply_script,
            post_chat_message=self.post_chat_message_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_teams.teamsmock")

__all__ = [
    "Mock",
    "list_thread_replies_read",
    "list_thread_replies_not_found",
    "list_thread_replies_provider_rejected",
    "list_thread_replies_invalid_response",
    "list_thread_replies_defect",
    "list_thread_replies_retry",
    "list_thread_replies_incident_acknowledgement",
    "list_thread_replies_missing_thread",
    "list_thread_replies_read_denied",
    "list_thread_replies_invalid_page",
    "post_channel_message_sent",
    "post_channel_message_provider_rejected",
    "post_channel_message_uncertain",
    "post_channel_message_defect",
    "post_channel_message_retry",
    "post_channel_message_incident_posted",
    "post_channel_message_channel_post_denied",
    "post_thread_reply_sent",
    "post_thread_reply_provider_rejected",
    "post_thread_reply_uncertain",
    "post_thread_reply_defect",
    "post_thread_reply_retry",
    "post_thread_reply_incident_status_reply",
    "post_thread_reply_reply_denied",
    "post_chat_message_sent",
    "post_chat_message_provider_rejected",
    "post_chat_message_uncertain",
    "post_chat_message_defect",
    "post_chat_message_retry",
    "post_chat_message_incident_escalation",
    "post_chat_message_chat_post_denied",
]
