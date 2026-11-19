# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Contact, Conversation, ConversationChannel, ConversationDetails, ConversationMessage, ConversationParticipant, ConversationReply, ConversationRequesterInput, CreateConversationInput, CreateConversationOutput, FindContactByEmailInput, FindContactByEmailOutput, GetConversationInput, ReplyToConversationInput, SearchConversationsInput, SearchConversationsOutput, UpdateConversationInput, UpdateConversationOutput
from .definitions import CONNECTOR_ID, CREATED, CREATE_CONVERSATION_DEFINITION, DEFECT, FIND_CONTACT_BY_EMAIL_DEFINITION, FOUND, GET_CONVERSATION_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, REPLIED, REPLY_TO_CONVERSATION_DEFINITION, SEARCHED, SEARCH_CONVERSATIONS_DEFINITION, UNCERTAIN, UPDATED, UPDATE_CONVERSATION_DEFINITION

def search_conversations_searched(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_conversations_searched, '__module__', "dex_connectors.reamaze.reamazemock")

def search_conversations_provider_rejected(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_conversations_provider_rejected, '__module__', "dex_connectors.reamaze.reamazemock")

def search_conversations_invalid_response(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_conversations_invalid_response, '__module__', "dex_connectors.reamaze.reamazemock")

def search_conversations_defect(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_conversations_defect, '__module__', "dex_connectors.reamaze.reamazemock")

def search_conversations_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchConversationsOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_conversations_retry, '__module__', "dex_connectors.reamaze.reamazemock")

def search_conversations_searched_default_answer() -> Case[SearchConversationsOutput]:
    return Case.paged(SEARCHED, (SearchConversationsOutput(conversations=(Conversation(id='resolved-billing', status=2, tags=('billing-double-charge',), channel=ConversationChannel(slug='support', name='Support', type=1), requester=ConversationParticipant(name='Jane Smith', email='jane@acme.example.com'), created_at=datetime.fromisoformat("2026-01-20T12:00:00Z".replace('Z', '+00:00'))),), total_count=3, page_count=3, next_page=2), SearchConversationsOutput(conversations=(Conversation(id='other-tag', status=0, tags=('shipping',), channel=ConversationChannel(slug='support', name='Support', type=1), requester=ConversationParticipant(name='Jane Smith', email='jane@acme.example.com'), created_at=datetime.fromisoformat("2026-01-21T12:00:00Z".replace('Z', '+00:00'))),), total_count=3, page_count=3, next_page=3), SearchConversationsOutput(conversations=(Conversation(id='other-requester', status=0, tags=('billing-double-charge',), channel=ConversationChannel(slug='support', name='Support', type=1), requester=ConversationParticipant(name='Alex Reed', email='alex@acme.example.com'), created_at=datetime.fromisoformat("2026-01-22T12:00:00Z".replace('Z', '+00:00'))),), total_count=3, page_count=3, next_page=0)))

setattr(search_conversations_searched_default_answer, '__module__', "dex_connectors.reamaze.reamazemock")

def search_conversations_provider_rejected_failure() -> Case[SearchConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchConversationsOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "searchConversations", "Re:amaze rejected the search (HTTP 401)."))

setattr(search_conversations_provider_rejected_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def search_conversations_invalid_response_failure() -> Case[SearchConversationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, SearchConversationsOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "searchConversations", "Re:amaze returned a malformed JSON response."))

setattr(search_conversations_invalid_response_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_conversation_found, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_not_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_conversation_not_found, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_provider_rejected(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_conversation_provider_rejected, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_invalid_response(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_conversation_invalid_response, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_defect(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_conversation_defect, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[ConversationDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_conversation_retry, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_found_default_answer() -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, ConversationDetails(conversation=Conversation(id='double-charge', subject='Double charge on order 88213', status=0, tags=('billing-double-charge',), channel=ConversationChannel(slug='support', name='Support', type=1), requester=ConversationParticipant(name='Jane Smith', email='jane@acme.example.com'), first_message='I was charged twice.', created_at=datetime.fromisoformat("2026-01-26T22:02:00Z".replace('Z', '+00:00'))), messages=(ConversationMessage(body='I was charged twice.', visibility=0, is_internal_note=False, origin=7, origin_id='customer-88213', created_at=datetime.fromisoformat("2026-01-26T22:02:00Z".replace('Z', '+00:00'))),)))

setattr(get_conversation_found_default_answer, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_not_found_failure() -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, ConversationDetails(conversation=Conversation(status=0, channel=ConversationChannel())), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getConversation", "The conversation does not exist in the brand or is not visible to the connection's user."))

setattr(get_conversation_not_found_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_provider_rejected_failure() -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, ConversationDetails(conversation=Conversation(status=0, channel=ConversationChannel())), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getConversation", "Re:amaze rejected the conversation read (HTTP 401)."))

setattr(get_conversation_provider_rejected_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def get_conversation_invalid_response_failure() -> Case[ConversationDetails]:
    return Case.branch_result(INVALID_RESPONSE, ConversationDetails(conversation=Conversation(status=0, channel=ConversationChannel())), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getConversation", "Re:amaze returned a malformed JSON response."))

setattr(get_conversation_invalid_response_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def create_conversation_created(value: CreateConversationOutput, failure: Failure | None = None) -> Case[CreateConversationOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_conversation_created, '__module__', "dex_connectors.reamaze.reamazemock")

def create_conversation_provider_rejected(value: CreateConversationOutput, failure: Failure | None = None) -> Case[CreateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_conversation_provider_rejected, '__module__', "dex_connectors.reamaze.reamazemock")

def create_conversation_uncertain(value: CreateConversationOutput, failure: Failure) -> Case[CreateConversationOutput]:
    return Case.uncertain(value, failure)

setattr(create_conversation_uncertain, '__module__', "dex_connectors.reamaze.reamazemock")

def create_conversation_defect(value: CreateConversationOutput, failure: Failure | None = None) -> Case[CreateConversationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_conversation_defect, '__module__', "dex_connectors.reamaze.reamazemock")

def create_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateConversationOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_conversation_retry, '__module__', "dex_connectors.reamaze.reamazemock")

def create_conversation_created_default_answer() -> Case[CreateConversationOutput]:
    return Case.branch_result(CREATED, CreateConversationOutput(conversation=Conversation(id='double-charge', subject='Double charge on order 88213', status=0, tags=('billing-double-charge',), channel=ConversationChannel(slug='support', name='Support', type=1), requester=ConversationParticipant(name='Jane Smith', email='jane@acme.example.com'), first_message='I was charged twice.', created_at=datetime.fromisoformat("2026-01-26T22:02:00Z".replace('Z', '+00:00')))))

setattr(create_conversation_created_default_answer, '__module__', "dex_connectors.reamaze.reamazemock")

def create_conversation_provider_rejected_failure() -> Case[CreateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateConversationOutput(conversation=Conversation(status=0, channel=ConversationChannel())), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createConversation", "Re:amaze rejected the channel (HTTP 422)."))

setattr(create_conversation_provider_rejected_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_updated(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_conversation_updated, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_not_found(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_conversation_not_found, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_provider_rejected(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_conversation_provider_rejected, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_invalid_response(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_conversation_invalid_response, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_defect(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_conversation_defect, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateConversationOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_conversation_retry, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_updated_default_answer() -> Case[UpdateConversationOutput]:
    return Case.branch_result(UPDATED, UpdateConversationOutput(conversation=Conversation(id='double-charge', subject='Double charge on order 88213', status=0, tags=('billing-double-charge',), channel=ConversationChannel(slug='support', name='Support', type=1), requester=ConversationParticipant(name='Jane Smith', email='jane@acme.example.com'), first_message='I was charged twice.', created_at=datetime.fromisoformat("2026-01-26T22:02:00Z".replace('Z', '+00:00')))))

setattr(update_conversation_updated_default_answer, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_not_found_failure() -> Case[UpdateConversationOutput]:
    return Case.branch_result(NOT_FOUND, UpdateConversationOutput(conversation=Conversation(status=0, channel=ConversationChannel())), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateConversation", "The conversation does not exist in the brand or is not visible to the connection's user."))

setattr(update_conversation_not_found_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_provider_rejected_failure() -> Case[UpdateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateConversationOutput(conversation=Conversation(status=0, channel=ConversationChannel())), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updateConversation", "Re:amaze rejected the assignee (HTTP 422)."))

setattr(update_conversation_provider_rejected_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def update_conversation_invalid_response_failure() -> Case[UpdateConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateConversationOutput(conversation=Conversation(status=0, channel=ConversationChannel())), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "updateConversation", "Re:amaze returned a malformed JSON response."))

setattr(update_conversation_invalid_response_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def reply_to_conversation_replied(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(REPLIED, value, failure)

setattr(reply_to_conversation_replied, '__module__', "dex_connectors.reamaze.reamazemock")

def reply_to_conversation_not_found(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(reply_to_conversation_not_found, '__module__', "dex_connectors.reamaze.reamazemock")

def reply_to_conversation_provider_rejected(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(reply_to_conversation_provider_rejected, '__module__', "dex_connectors.reamaze.reamazemock")

def reply_to_conversation_uncertain(value: ConversationReply, failure: Failure) -> Case[ConversationReply]:
    return Case.uncertain(value, failure)

setattr(reply_to_conversation_uncertain, '__module__', "dex_connectors.reamaze.reamazemock")

def reply_to_conversation_defect(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(DEFECT, value, failure)

setattr(reply_to_conversation_defect, '__module__', "dex_connectors.reamaze.reamazemock")

def reply_to_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[ConversationReply]:
    return Case.retry(failure, after_seconds)

setattr(reply_to_conversation_retry, '__module__', "dex_connectors.reamaze.reamazemock")

def reply_to_conversation_replied_default_answer() -> Case[ConversationReply]:
    return Case.branch_result(REPLIED, ConversationReply(conversation_id='double-charge', message=ConversationMessage(body='Triage note for order 88213.', visibility=1, is_internal_note=True, origin=7, origin_id='dex-note-88213', created_at=datetime.fromisoformat("2026-01-27T20:00:00Z".replace('Z', '+00:00')))))

setattr(reply_to_conversation_replied_default_answer, '__module__', "dex_connectors.reamaze.reamazemock")

def reply_to_conversation_not_found_failure() -> Case[ConversationReply]:
    return Case.branch_result(NOT_FOUND, ConversationReply(message=ConversationMessage(visibility=0)), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "replyToConversation", "The conversation does not exist in the brand or is not visible to the connection's user; nothing was added."))

setattr(reply_to_conversation_not_found_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def reply_to_conversation_provider_rejected_failure() -> Case[ConversationReply]:
    return Case.branch_result(PROVIDER_REJECTED, ConversationReply(message=ConversationMessage(visibility=0)), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "replyToConversation", "Re:amaze rejected the message (HTTP 401)."))

setattr(reply_to_conversation_provider_rejected_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_found(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_contact_by_email_found, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_not_found(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_contact_by_email_not_found, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_provider_rejected(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_contact_by_email_provider_rejected, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_invalid_response(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_contact_by_email_invalid_response, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_defect(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_contact_by_email_defect, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[FindContactByEmailOutput]:
    return Case.retry(failure, after_seconds)

setattr(find_contact_by_email_retry, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_found_default_answer() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(FOUND, FindContactByEmailOutput(contacts=(Contact(name='Jane Smith', email='jane@acme.example.com', friendly_name='Jane'),)))

setattr(find_contact_by_email_found_default_answer, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_not_found_no_match() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(NOT_FOUND, FindContactByEmailOutput(contacts=(), has_more_candidates=False))

setattr(find_contact_by_email_not_found_no_match, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_provider_rejected_failure() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, FindContactByEmailOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "findContactByEmail", "Re:amaze rejected the contact search (HTTP 401)."))

setattr(find_contact_by_email_provider_rejected_failure, '__module__', "dex_connectors.reamaze.reamazemock")

def find_contact_by_email_invalid_response_failure() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(INVALID_RESPONSE, FindContactByEmailOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "findContactByEmail", "Re:amaze returned a malformed JSON response."))

setattr(find_contact_by_email_invalid_response_failure, '__module__', "dex_connectors.reamaze.reamazemock")


class Mock:

    def __init__(self, connection_name: str = "reamaze") -> None:
        self.search_conversations_script: Query[SearchConversationsInput, SearchConversationsOutput] = Query(SEARCH_CONVERSATIONS_DEFINITION)
        self.search_conversations_script.default(search_conversations_searched_default_answer())
        self.get_conversation_script: Query[GetConversationInput, ConversationDetails] = Query(GET_CONVERSATION_DEFINITION)
        self.get_conversation_script.default(get_conversation_found_default_answer())
        self.create_conversation_script: Mutation[CreateConversationInput, CreateConversationOutput] = Mutation(CREATE_CONVERSATION_DEFINITION)
        self.create_conversation_script.default(create_conversation_created_default_answer())
        self.update_conversation_script: Mutation[UpdateConversationInput, UpdateConversationOutput] = Mutation(UPDATE_CONVERSATION_DEFINITION)
        self.update_conversation_script.default(update_conversation_updated_default_answer())
        self.reply_to_conversation_script: Mutation[ReplyToConversationInput, ConversationReply] = Mutation(REPLY_TO_CONVERSATION_DEFINITION)
        self.reply_to_conversation_script.default(reply_to_conversation_replied_default_answer())
        self.find_contact_by_email_script: Query[FindContactByEmailInput, FindContactByEmailOutput] = Query(FIND_CONTACT_BY_EMAIL_DEFINITION)
        self.find_contact_by_email_script.default(find_contact_by_email_found_default_answer())
        self._connection = new_connection_with_operations(
            search_conversations=self.search_conversations_script,
            get_conversation=self.get_conversation_script,
            create_conversation=self.create_conversation_script,
            update_conversation=self.update_conversation_script,
            reply_to_conversation=self.reply_to_conversation_script,
            find_contact_by_email=self.find_contact_by_email_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.reamaze.reamazemock")

__all__ = [
    "Mock",
    "search_conversations_searched",
    "search_conversations_provider_rejected",
    "search_conversations_invalid_response",
    "search_conversations_defect",
    "search_conversations_retry",
    "search_conversations_searched_default_answer",
    "search_conversations_provider_rejected_failure",
    "search_conversations_invalid_response_failure",
    "get_conversation_found",
    "get_conversation_not_found",
    "get_conversation_provider_rejected",
    "get_conversation_invalid_response",
    "get_conversation_defect",
    "get_conversation_retry",
    "get_conversation_found_default_answer",
    "get_conversation_not_found_failure",
    "get_conversation_provider_rejected_failure",
    "get_conversation_invalid_response_failure",
    "create_conversation_created",
    "create_conversation_provider_rejected",
    "create_conversation_uncertain",
    "create_conversation_defect",
    "create_conversation_retry",
    "create_conversation_created_default_answer",
    "create_conversation_provider_rejected_failure",
    "update_conversation_updated",
    "update_conversation_not_found",
    "update_conversation_provider_rejected",
    "update_conversation_invalid_response",
    "update_conversation_defect",
    "update_conversation_retry",
    "update_conversation_updated_default_answer",
    "update_conversation_not_found_failure",
    "update_conversation_provider_rejected_failure",
    "update_conversation_invalid_response_failure",
    "reply_to_conversation_replied",
    "reply_to_conversation_not_found",
    "reply_to_conversation_provider_rejected",
    "reply_to_conversation_uncertain",
    "reply_to_conversation_defect",
    "reply_to_conversation_retry",
    "reply_to_conversation_replied_default_answer",
    "reply_to_conversation_not_found_failure",
    "reply_to_conversation_provider_rejected_failure",
    "find_contact_by_email_found",
    "find_contact_by_email_not_found",
    "find_contact_by_email_provider_rejected",
    "find_contact_by_email_invalid_response",
    "find_contact_by_email_defect",
    "find_contact_by_email_retry",
    "find_contact_by_email_found_default_answer",
    "find_contact_by_email_not_found_no_match",
    "find_contact_by_email_provider_rejected_failure",
    "find_contact_by_email_invalid_response_failure",
]
