# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Contact, Conversation, ConversationAuthor, ConversationContact, ConversationDetails, ConversationPart, ConversationSource, ConversationTag, FindContactByEmailInput, FindContactByEmailOutput, GetConversationInput, ReplyToConversationInput, ReplyToConversationOutput, SearchConversationsInput, SearchConversationsOutput, UpdateConversationStateInput, UpdateConversationStateOutput
from .definitions import CONNECTOR_ID, DEFECT, FIND_CONTACT_BY_EMAIL_DEFINITION, FOUND, GET_CONVERSATION_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, REPLIED, REPLY_TO_CONVERSATION_DEFINITION, SEARCHED, SEARCH_CONVERSATIONS_DEFINITION, UNCERTAIN, UPDATED, UPDATE_CONVERSATION_STATE_DEFINITION

def search_conversations_searched(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_conversations_searched, '__module__', "dex_connectors.intercom.intercommock")

def search_conversations_provider_rejected(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_conversations_provider_rejected, '__module__', "dex_connectors.intercom.intercommock")

def search_conversations_invalid_response(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_conversations_invalid_response, '__module__', "dex_connectors.intercom.intercommock")

def search_conversations_defect(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_conversations_defect, '__module__', "dex_connectors.intercom.intercommock")

def search_conversations_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchConversationsOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_conversations_retry, '__module__', "dex_connectors.intercom.intercommock")

def search_conversations_ada_open_conversations() -> Case[SearchConversationsOutput]:
    return Case.paged(SEARCHED, (SearchConversationsOutput(conversations=(Conversation(id='2002', title='Duplicate refund question', state='open', is_open=True, source=ConversationSource(type='conversation', body='I need help with my refund', author=ConversationAuthor(type='user', id='contact-1', name='Ada Lovelace', email='ada@example.com')), contacts=(ConversationContact(id='contact-1'),), created_at=datetime.fromisoformat("2026-10-26T10:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-26T10:00:00Z".replace('Z', '+00:00'))), Conversation(id='2001', title='Original refund question', state='open', is_open=True, source=ConversationSource(type='conversation', author=ConversationAuthor(type='user', id='contact-1', email='ada@example.com')), contacts=(ConversationContact(id='contact-1'),), created_at=datetime.fromisoformat("2026-10-25T10:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T12:00:00Z".replace('Z', '+00:00')))), total_count=3, next_cursor='conversations-page-2'), SearchConversationsOutput(conversations=(Conversation(id='2001', title='Original refund question', state='open', is_open=True, source=ConversationSource(type='conversation', author=ConversationAuthor(type='user', id='contact-1', email='ada@example.com')), contacts=(ConversationContact(id='contact-1'),), created_at=datetime.fromisoformat("2026-10-25T10:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T12:00:00Z".replace('Z', '+00:00'))), Conversation(id='2003', title='Later follow-up', state='open', is_open=True, source=ConversationSource(type='conversation', author=ConversationAuthor(type='user', id='contact-1', email='ada@example.com')), contacts=(ConversationContact(id='contact-1'),), created_at=datetime.fromisoformat("2026-10-26T11:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-26T11:00:00Z".replace('Z', '+00:00')))), total_count=3, next_cursor='conversations-page-3'), SearchConversationsOutput(conversations=(), total_count=3)))

setattr(search_conversations_ada_open_conversations, '__module__', "dex_connectors.intercom.intercommock")

def search_conversations_provider_rejected_failure() -> Case[SearchConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchConversationsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchConversations", "Intercom denied the requested operation."))

setattr(search_conversations_provider_rejected_failure, '__module__', "dex_connectors.intercom.intercommock")

def get_conversation_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_conversation_found, '__module__', "dex_connectors.intercom.intercommock")

def get_conversation_not_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_conversation_not_found, '__module__', "dex_connectors.intercom.intercommock")

def get_conversation_provider_rejected(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_conversation_provider_rejected, '__module__', "dex_connectors.intercom.intercommock")

def get_conversation_invalid_response(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_conversation_invalid_response, '__module__', "dex_connectors.intercom.intercommock")

def get_conversation_defect(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_conversation_defect, '__module__', "dex_connectors.intercom.intercommock")

def get_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[ConversationDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_conversation_retry, '__module__', "dex_connectors.intercom.intercommock")

def get_conversation_ada_inbound_conversation() -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, ConversationDetails(conversation=Conversation(id='2002', title='Duplicate refund question', state='open', is_open=True, source=ConversationSource(type='conversation', body='I need help with my refund', author=ConversationAuthor(type='user', id='contact-1', name='Ada Lovelace', email='ada@example.com')), contacts=(ConversationContact(id='contact-1'),), created_at=datetime.fromisoformat("2026-10-26T10:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-26T10:00:00Z".replace('Z', '+00:00'))), latest_parts=(), part_count=0))

setattr(get_conversation_ada_inbound_conversation, '__module__', "dex_connectors.intercom.intercommock")

def get_conversation_not_found_failure() -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, ConversationDetails(conversation=Conversation(state='', source=ConversationSource(author=ConversationAuthor()))), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getConversation", "The conversation does not exist or is not visible to the connection's token."))

setattr(get_conversation_not_found_failure, '__module__', "dex_connectors.intercom.intercommock")

def get_conversation_provider_rejected_failure() -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, ConversationDetails(conversation=Conversation(state='', source=ConversationSource(author=ConversationAuthor()))), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getConversation", "Intercom denied the requested operation."))

setattr(get_conversation_provider_rejected_failure, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_replied(value: ReplyToConversationOutput, failure: Failure | None = None) -> Case[ReplyToConversationOutput]:
    return Case.branch_result(REPLIED, value, failure)

setattr(reply_to_conversation_replied, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_not_found(value: ReplyToConversationOutput, failure: Failure | None = None) -> Case[ReplyToConversationOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(reply_to_conversation_not_found, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_provider_rejected(value: ReplyToConversationOutput, failure: Failure | None = None) -> Case[ReplyToConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(reply_to_conversation_provider_rejected, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_uncertain(value: ReplyToConversationOutput, failure: Failure) -> Case[ReplyToConversationOutput]:
    return Case.uncertain(value, failure)

setattr(reply_to_conversation_uncertain, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_invalid_response(value: ReplyToConversationOutput, failure: Failure | None = None) -> Case[ReplyToConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(reply_to_conversation_invalid_response, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_defect(value: ReplyToConversationOutput, failure: Failure | None = None) -> Case[ReplyToConversationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(reply_to_conversation_defect, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[ReplyToConversationOutput]:
    return Case.retry(failure, after_seconds)

setattr(reply_to_conversation_retry, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_duplicate_answered() -> Case[ReplyToConversationOutput]:
    return Case.branch_result(REPLIED, ReplyToConversationOutput(conversation_id='2002', part=ConversationPart(id='3001', part_type='comment', body='We are working on your earlier conversation', author=ConversationAuthor(type='admin', id='77'), created_at=datetime.fromisoformat("2026-10-26T10:05:00Z".replace('Z', '+00:00'))), conversation_state='open', was_already_applied=False))

setattr(reply_to_conversation_duplicate_answered, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_not_found_failure() -> Case[ReplyToConversationOutput]:
    return Case.branch_result(NOT_FOUND, ReplyToConversationOutput(conversation_id='2002', part=ConversationPart(author=ConversationAuthor())), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "replyToConversation", "The conversation does not exist or is not visible to the connection's token; nothing was added."))

setattr(reply_to_conversation_not_found_failure, '__module__', "dex_connectors.intercom.intercommock")

def reply_to_conversation_provider_rejected_failure() -> Case[ReplyToConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ReplyToConversationOutput(conversation_id='2002', part=ConversationPart(author=ConversationAuthor())), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "replyToConversation", "Intercom denied the requested operation."))

setattr(reply_to_conversation_provider_rejected_failure, '__module__', "dex_connectors.intercom.intercommock")

def update_conversation_state_updated(value: UpdateConversationStateOutput, failure: Failure | None = None) -> Case[UpdateConversationStateOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_conversation_state_updated, '__module__', "dex_connectors.intercom.intercommock")

def update_conversation_state_not_found(value: UpdateConversationStateOutput, failure: Failure | None = None) -> Case[UpdateConversationStateOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_conversation_state_not_found, '__module__', "dex_connectors.intercom.intercommock")

def update_conversation_state_provider_rejected(value: UpdateConversationStateOutput, failure: Failure | None = None) -> Case[UpdateConversationStateOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_conversation_state_provider_rejected, '__module__', "dex_connectors.intercom.intercommock")

def update_conversation_state_invalid_response(value: UpdateConversationStateOutput, failure: Failure | None = None) -> Case[UpdateConversationStateOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_conversation_state_invalid_response, '__module__', "dex_connectors.intercom.intercommock")

def update_conversation_state_defect(value: UpdateConversationStateOutput, failure: Failure | None = None) -> Case[UpdateConversationStateOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_conversation_state_defect, '__module__', "dex_connectors.intercom.intercommock")

def update_conversation_state_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateConversationStateOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_conversation_state_retry, '__module__', "dex_connectors.intercom.intercommock")

def update_conversation_state_duplicate_closed() -> Case[UpdateConversationStateOutput]:
    return Case.branch_result(UPDATED, UpdateConversationStateOutput(conversation=Conversation(id='2002', title='Duplicate refund question', state='closed', is_open=False, source=ConversationSource(type='conversation', body='I need help with my refund', author=ConversationAuthor(type='user', id='contact-1', name='Ada Lovelace', email='ada@example.com')), contacts=(ConversationContact(id='contact-1'),), created_at=datetime.fromisoformat("2026-10-26T10:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-26T10:10:00Z".replace('Z', '+00:00'))), was_already_applied=False))

setattr(update_conversation_state_duplicate_closed, '__module__', "dex_connectors.intercom.intercommock")

def update_conversation_state_not_found_failure() -> Case[UpdateConversationStateOutput]:
    return Case.branch_result(NOT_FOUND, UpdateConversationStateOutput(conversation=Conversation(state='', source=ConversationSource(author=ConversationAuthor()))), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateConversationState", "The conversation does not exist or is not visible to the connection's token."))

setattr(update_conversation_state_not_found_failure, '__module__', "dex_connectors.intercom.intercommock")

def update_conversation_state_provider_rejected_failure() -> Case[UpdateConversationStateOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateConversationStateOutput(conversation=Conversation(state='', source=ConversationSource(author=ConversationAuthor()))), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateConversationState", "Intercom denied the requested operation."))

setattr(update_conversation_state_provider_rejected_failure, '__module__', "dex_connectors.intercom.intercommock")

def find_contact_by_email_found(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_contact_by_email_found, '__module__', "dex_connectors.intercom.intercommock")

def find_contact_by_email_not_found(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_contact_by_email_not_found, '__module__', "dex_connectors.intercom.intercommock")

def find_contact_by_email_provider_rejected(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_contact_by_email_provider_rejected, '__module__', "dex_connectors.intercom.intercommock")

def find_contact_by_email_invalid_response(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_contact_by_email_invalid_response, '__module__', "dex_connectors.intercom.intercommock")

def find_contact_by_email_defect(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_contact_by_email_defect, '__module__', "dex_connectors.intercom.intercommock")

def find_contact_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[FindContactByEmailOutput]:
    return Case.retry(failure, after_seconds)

setattr(find_contact_by_email_retry, '__module__', "dex_connectors.intercom.intercommock")

def find_contact_by_email_ada_contact() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(FOUND, FindContactByEmailOutput(contacts=(Contact(id='contact-1', role='user', email='ada@example.com', name='Ada Lovelace', created_at=datetime.fromisoformat("2026-01-01T00:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T10:00:00Z".replace('Z', '+00:00'))),)))

setattr(find_contact_by_email_ada_contact, '__module__', "dex_connectors.intercom.intercommock")

def find_contact_by_email_not_found_failure() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(NOT_FOUND, FindContactByEmailOutput(contacts=()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "findContactByEmail", "No Intercom contact has the email address."))

setattr(find_contact_by_email_not_found_failure, '__module__', "dex_connectors.intercom.intercommock")

def find_contact_by_email_provider_rejected_failure() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, FindContactByEmailOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findContactByEmail", "Intercom denied the requested operation."))

setattr(find_contact_by_email_provider_rejected_failure, '__module__', "dex_connectors.intercom.intercommock")


class Mock:

    def __init__(self, connection_name: str = "intercom") -> None:
        self.search_conversations_script: Query[SearchConversationsInput, SearchConversationsOutput] = Query(SEARCH_CONVERSATIONS_DEFINITION)
        self.search_conversations_script.default(search_conversations_ada_open_conversations())
        self.get_conversation_script: Query[GetConversationInput, ConversationDetails] = Query(GET_CONVERSATION_DEFINITION)
        self.get_conversation_script.default(get_conversation_ada_inbound_conversation())
        self.reply_to_conversation_script: Mutation[ReplyToConversationInput, ReplyToConversationOutput] = Mutation(REPLY_TO_CONVERSATION_DEFINITION)
        self.reply_to_conversation_script.default(reply_to_conversation_duplicate_answered())
        self.update_conversation_state_script: Mutation[UpdateConversationStateInput, UpdateConversationStateOutput] = Mutation(UPDATE_CONVERSATION_STATE_DEFINITION)
        self.update_conversation_state_script.default(update_conversation_state_duplicate_closed())
        self.find_contact_by_email_script: Query[FindContactByEmailInput, FindContactByEmailOutput] = Query(FIND_CONTACT_BY_EMAIL_DEFINITION)
        self.find_contact_by_email_script.default(find_contact_by_email_ada_contact())
        self._connection = new_connection_with_operations(
            search_conversations=self.search_conversations_script,
            get_conversation=self.get_conversation_script,
            reply_to_conversation=self.reply_to_conversation_script,
            update_conversation_state=self.update_conversation_state_script,
            find_contact_by_email=self.find_contact_by_email_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.intercom.intercommock")

__all__ = [
    "Mock",
    "search_conversations_searched",
    "search_conversations_provider_rejected",
    "search_conversations_invalid_response",
    "search_conversations_defect",
    "search_conversations_retry",
    "search_conversations_ada_open_conversations",
    "search_conversations_provider_rejected_failure",
    "get_conversation_found",
    "get_conversation_not_found",
    "get_conversation_provider_rejected",
    "get_conversation_invalid_response",
    "get_conversation_defect",
    "get_conversation_retry",
    "get_conversation_ada_inbound_conversation",
    "get_conversation_not_found_failure",
    "get_conversation_provider_rejected_failure",
    "reply_to_conversation_replied",
    "reply_to_conversation_not_found",
    "reply_to_conversation_provider_rejected",
    "reply_to_conversation_uncertain",
    "reply_to_conversation_invalid_response",
    "reply_to_conversation_defect",
    "reply_to_conversation_retry",
    "reply_to_conversation_duplicate_answered",
    "reply_to_conversation_not_found_failure",
    "reply_to_conversation_provider_rejected_failure",
    "update_conversation_state_updated",
    "update_conversation_state_not_found",
    "update_conversation_state_provider_rejected",
    "update_conversation_state_invalid_response",
    "update_conversation_state_defect",
    "update_conversation_state_retry",
    "update_conversation_state_duplicate_closed",
    "update_conversation_state_not_found_failure",
    "update_conversation_state_provider_rejected_failure",
    "find_contact_by_email_found",
    "find_contact_by_email_not_found",
    "find_contact_by_email_provider_rejected",
    "find_contact_by_email_invalid_response",
    "find_contact_by_email_defect",
    "find_contact_by_email_retry",
    "find_contact_by_email_ada_contact",
    "find_contact_by_email_not_found_failure",
    "find_contact_by_email_provider_rejected_failure",
]
