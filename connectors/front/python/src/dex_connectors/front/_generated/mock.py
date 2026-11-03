# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Comment, Contact, ContactHandle, Conversation, ConversationDetails, ConversationReply, FindContactByEmailInput, FindContactByEmailOutput, GetConversationInput, Message, Recipient, ReplyToConversationInput, SearchConversationsInput, SearchConversationsOutput, Tag, Teammate, UpdateConversationInput, UpdateConversationOutput
from .definitions import CONNECTOR_ID, DEFECT, FIND_CONTACT_BY_EMAIL_DEFINITION, FOUND, GET_CONVERSATION_DEFINITION, INVALID_RESPONSE, MERGED, NOT_FOUND, PROVIDER_REJECTED, REPLIED, REPLY_TO_CONVERSATION_DEFINITION, SEARCHED, SEARCH_CONVERSATIONS_DEFINITION, UNCERTAIN, UPDATED, UPDATE_CONVERSATION_DEFINITION

def search_conversations_searched(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_conversations_searched, '__module__', "dex_connectors.front.frontmock")

def search_conversations_provider_rejected(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_conversations_provider_rejected, '__module__', "dex_connectors.front.frontmock")

def search_conversations_invalid_response(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_conversations_invalid_response, '__module__', "dex_connectors.front.frontmock")

def search_conversations_defect(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_conversations_defect, '__module__', "dex_connectors.front.frontmock")

def search_conversations_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchConversationsOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_conversations_retry, '__module__', "dex_connectors.front.frontmock")

def search_conversations_matching_conversations() -> Case[SearchConversationsOutput]:
    return Case.branch_result(SEARCHED, SearchConversationsOutput(conversations=(Conversation(id='cnv_100', type='conversation', subject='Order 88213', status='unassigned', created_at=datetime.fromisoformat("2026-01-02T03:04:05Z".replace('Z', '+00:00'))), Conversation(id='cnv_200', type='conversation', subject='Earlier order question', status='unassigned', created_at=datetime.fromisoformat("2026-01-01T03:04:05Z".replace('Z', '+00:00')))), total_count=2, query='inbox:inb_support is:open recipient:jane@acme.example.com'))

setattr(search_conversations_matching_conversations, '__module__', "dex_connectors.front.frontmock")

def search_conversations_invalid_token() -> Case[SearchConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchConversationsOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "searchConversations", "Front rejected the API token (HTTP 401)."))

setattr(search_conversations_invalid_token, '__module__', "dex_connectors.front.frontmock")

def search_conversations_oversized_page() -> Case[SearchConversationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, SearchConversationsOutput(), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "searchConversations", "The Front response exceeds maxResponseBytes."))

setattr(search_conversations_oversized_page, '__module__', "dex_connectors.front.frontmock")

def get_conversation_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_conversation_found, '__module__', "dex_connectors.front.frontmock")

def get_conversation_merged(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(MERGED, value, failure)

setattr(get_conversation_merged, '__module__', "dex_connectors.front.frontmock")

def get_conversation_not_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_conversation_not_found, '__module__', "dex_connectors.front.frontmock")

def get_conversation_provider_rejected(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_conversation_provider_rejected, '__module__', "dex_connectors.front.frontmock")

def get_conversation_invalid_response(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_conversation_invalid_response, '__module__', "dex_connectors.front.frontmock")

def get_conversation_defect(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_conversation_defect, '__module__', "dex_connectors.front.frontmock")

def get_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[ConversationDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_conversation_retry, '__module__', "dex_connectors.front.frontmock")

def get_conversation_triage_conversation() -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, ConversationDetails(conversation=Conversation(id='cnv_100', type='conversation', subject='Order 88213', status='unassigned', created_at=datetime.fromisoformat("2026-01-02T03:04:05Z".replace('Z', '+00:00'))), messages=(Message(id='msg_100', type='email', is_inbound=True, body='Any update on my refund?', recipients=(Recipient(handle='jane@acme.example.com', role='from'),), created_at=datetime.fromisoformat("2026-01-02T03:04:05Z".replace('Z', '+00:00'))),), comments=()))

setattr(get_conversation_triage_conversation, '__module__', "dex_connectors.front.frontmock")

def get_conversation_merged_conversation() -> Case[ConversationDetails]:
    return Case.branch_result(MERGED, ConversationDetails(conversation=Conversation(status=''), merged_into_conversation_id='cnv_200'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getConversation", "The conversation was merged into cnv_200."))

setattr(get_conversation_merged_conversation, '__module__', "dex_connectors.front.frontmock")

def get_conversation_missing_conversation() -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, ConversationDetails(conversation=Conversation(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getConversation", "Front found no such conversation (HTTP 404)."))

setattr(get_conversation_missing_conversation, '__module__', "dex_connectors.front.frontmock")

def get_conversation_denied_conversation_read() -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, ConversationDetails(conversation=Conversation(status='')), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getConversation", "Front denied message read access (HTTP 403)."))

setattr(get_conversation_denied_conversation_read, '__module__', "dex_connectors.front.frontmock")

def get_conversation_oversized_conversation() -> Case[ConversationDetails]:
    return Case.branch_result(INVALID_RESPONSE, ConversationDetails(conversation=Conversation(status='')), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "getConversation", "The Front response exceeds maxResponseBytes."))

setattr(get_conversation_oversized_conversation, '__module__', "dex_connectors.front.frontmock")

def reply_to_conversation_replied(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(REPLIED, value, failure)

setattr(reply_to_conversation_replied, '__module__', "dex_connectors.front.frontmock")

def reply_to_conversation_not_found(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(reply_to_conversation_not_found, '__module__', "dex_connectors.front.frontmock")

def reply_to_conversation_provider_rejected(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(reply_to_conversation_provider_rejected, '__module__', "dex_connectors.front.frontmock")

def reply_to_conversation_uncertain(value: ConversationReply, failure: Failure) -> Case[ConversationReply]:
    return Case.uncertain(value, failure)

setattr(reply_to_conversation_uncertain, '__module__', "dex_connectors.front.frontmock")

def reply_to_conversation_defect(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(DEFECT, value, failure)

setattr(reply_to_conversation_defect, '__module__', "dex_connectors.front.frontmock")

def reply_to_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[ConversationReply]:
    return Case.retry(failure, after_seconds)

setattr(reply_to_conversation_retry, '__module__', "dex_connectors.front.frontmock")

def reply_to_conversation_triage_comment() -> Case[ConversationReply]:
    return Case.branch_result(REPLIED, ConversationReply(conversation_id='cnv_100', is_internal_note=True, comment_id='com_100'))

setattr(reply_to_conversation_triage_comment, '__module__', "dex_connectors.front.frontmock")

def reply_to_conversation_missing_conversation() -> Case[ConversationReply]:
    return Case.branch_result(NOT_FOUND, ConversationReply(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "replyToConversation", "Front found no such conversation (HTTP 404)."))

setattr(reply_to_conversation_missing_conversation, '__module__', "dex_connectors.front.frontmock")

def reply_to_conversation_denied_comment_write() -> Case[ConversationReply]:
    return Case.branch_result(PROVIDER_REJECTED, ConversationReply(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "replyToConversation", "Front denied comment write access (HTTP 403)."))

setattr(reply_to_conversation_denied_comment_write, '__module__', "dex_connectors.front.frontmock")

def update_conversation_updated(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_conversation_updated, '__module__', "dex_connectors.front.frontmock")

def update_conversation_not_found(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_conversation_not_found, '__module__', "dex_connectors.front.frontmock")

def update_conversation_provider_rejected(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_conversation_provider_rejected, '__module__', "dex_connectors.front.frontmock")

def update_conversation_invalid_response(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_conversation_invalid_response, '__module__', "dex_connectors.front.frontmock")

def update_conversation_defect(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_conversation_defect, '__module__', "dex_connectors.front.frontmock")

def update_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateConversationOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_conversation_retry, '__module__', "dex_connectors.front.frontmock")

def update_conversation_routed_conversation() -> Case[UpdateConversationOutput]:
    return Case.branch_result(UPDATED, UpdateConversationOutput(conversation=Conversation(id='cnv_100', type='conversation', subject='Order 88213', status='assigned', assignee=Teammate(id='tea_leela'), tags=(Tag(id='tag_triaged', name='triaged'),), created_at=datetime.fromisoformat("2026-01-02T03:04:05Z".replace('Z', '+00:00')))))

setattr(update_conversation_routed_conversation, '__module__', "dex_connectors.front.frontmock")

def update_conversation_missing_conversation() -> Case[UpdateConversationOutput]:
    return Case.branch_result(NOT_FOUND, UpdateConversationOutput(conversation=Conversation(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateConversation", "Front found no such conversation (HTTP 404)."))

setattr(update_conversation_missing_conversation, '__module__', "dex_connectors.front.frontmock")

def update_conversation_invalid_tag() -> Case[UpdateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateConversationOutput(conversation=Conversation(status='')), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updateConversation", "Front rejected the tag ID (HTTP 400)."))

setattr(update_conversation_invalid_tag, '__module__', "dex_connectors.front.frontmock")

def update_conversation_oversized_read_back() -> Case[UpdateConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateConversationOutput(conversation=Conversation(status='')), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "updateConversation", "The Front response exceeds maxResponseBytes."))

setattr(update_conversation_oversized_read_back, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_found(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_contact_by_email_found, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_not_found(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_contact_by_email_not_found, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_provider_rejected(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_contact_by_email_provider_rejected, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_invalid_response(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_contact_by_email_invalid_response, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_defect(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_contact_by_email_defect, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[FindContactByEmailOutput]:
    return Case.retry(failure, after_seconds)

setattr(find_contact_by_email_retry, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_requester_contact() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(FOUND, FindContactByEmailOutput(contacts=(Contact(id='crd_100', name='Jane Smith', handles=(ContactHandle(handle='jane@acme.example.com', source='email'),)),)))

setattr(find_contact_by_email_requester_contact, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_missing_contact() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(NOT_FOUND, FindContactByEmailOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "findContactByEmail", "Front found no contact for this email (HTTP 404)."))

setattr(find_contact_by_email_missing_contact, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_denied_contact_read() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, FindContactByEmailOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findContactByEmail", "Front denied contact read access (HTTP 403)."))

setattr(find_contact_by_email_denied_contact_read, '__module__', "dex_connectors.front.frontmock")

def find_contact_by_email_oversized_contact() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(INVALID_RESPONSE, FindContactByEmailOutput(), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "findContactByEmail", "The Front response exceeds maxResponseBytes."))

setattr(find_contact_by_email_oversized_contact, '__module__', "dex_connectors.front.frontmock")


class Mock:

    def __init__(self, connection_name: str = "front") -> None:
        self.search_conversations_script: Query[SearchConversationsInput, SearchConversationsOutput] = Query(SEARCH_CONVERSATIONS_DEFINITION)
        self.search_conversations_script.default(search_conversations_matching_conversations())
        self.get_conversation_script: Query[GetConversationInput, ConversationDetails] = Query(GET_CONVERSATION_DEFINITION)
        self.get_conversation_script.default(get_conversation_triage_conversation())
        self.reply_to_conversation_script: Mutation[ReplyToConversationInput, ConversationReply] = Mutation(REPLY_TO_CONVERSATION_DEFINITION)
        self.reply_to_conversation_script.default(reply_to_conversation_triage_comment())
        self.update_conversation_script: Mutation[UpdateConversationInput, UpdateConversationOutput] = Mutation(UPDATE_CONVERSATION_DEFINITION)
        self.update_conversation_script.default(update_conversation_routed_conversation())
        self.find_contact_by_email_script: Query[FindContactByEmailInput, FindContactByEmailOutput] = Query(FIND_CONTACT_BY_EMAIL_DEFINITION)
        self.find_contact_by_email_script.default(find_contact_by_email_requester_contact())
        self._connection = new_connection_with_operations(
            search_conversations=self.search_conversations_script,
            get_conversation=self.get_conversation_script,
            reply_to_conversation=self.reply_to_conversation_script,
            update_conversation=self.update_conversation_script,
            find_contact_by_email=self.find_contact_by_email_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.front.frontmock")

__all__ = [
    "Mock",
    "search_conversations_searched",
    "search_conversations_provider_rejected",
    "search_conversations_invalid_response",
    "search_conversations_defect",
    "search_conversations_retry",
    "search_conversations_matching_conversations",
    "search_conversations_invalid_token",
    "search_conversations_oversized_page",
    "get_conversation_found",
    "get_conversation_merged",
    "get_conversation_not_found",
    "get_conversation_provider_rejected",
    "get_conversation_invalid_response",
    "get_conversation_defect",
    "get_conversation_retry",
    "get_conversation_triage_conversation",
    "get_conversation_merged_conversation",
    "get_conversation_missing_conversation",
    "get_conversation_denied_conversation_read",
    "get_conversation_oversized_conversation",
    "reply_to_conversation_replied",
    "reply_to_conversation_not_found",
    "reply_to_conversation_provider_rejected",
    "reply_to_conversation_uncertain",
    "reply_to_conversation_defect",
    "reply_to_conversation_retry",
    "reply_to_conversation_triage_comment",
    "reply_to_conversation_missing_conversation",
    "reply_to_conversation_denied_comment_write",
    "update_conversation_updated",
    "update_conversation_not_found",
    "update_conversation_provider_rejected",
    "update_conversation_invalid_response",
    "update_conversation_defect",
    "update_conversation_retry",
    "update_conversation_routed_conversation",
    "update_conversation_missing_conversation",
    "update_conversation_invalid_tag",
    "update_conversation_oversized_read_back",
    "find_contact_by_email_found",
    "find_contact_by_email_not_found",
    "find_contact_by_email_provider_rejected",
    "find_contact_by_email_invalid_response",
    "find_contact_by_email_defect",
    "find_contact_by_email_retry",
    "find_contact_by_email_requester_contact",
    "find_contact_by_email_missing_contact",
    "find_contact_by_email_denied_contact_read",
    "find_contact_by_email_oversized_contact",
]
