# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Conversation, ConversationDetails, ConversationReply, Customer, CustomerMatches, FindCustomerByEmailInput, GetConversationInput, Person, ReplyToConversationInput, SearchConversationsInput, SearchConversationsOutput, Thread, UpdateConversationInput, UpdateConversationOutput
from .definitions import CONNECTOR_ID, DEFECT, FIND_CUSTOMER_BY_EMAIL_DEFINITION, FOUND, GET_CONVERSATION_DEFINITION, INVALID_RESPONSE, MERGED, NOT_FOUND, PROVIDER_REJECTED, REPLIED, REPLY_TO_CONVERSATION_DEFINITION, SEARCHED, SEARCH_CONVERSATIONS_DEFINITION, UNCERTAIN, UPDATED, UPDATE_CONVERSATION_DEFINITION

def search_conversations_searched(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_conversations_searched, '__module__', "dex_connectors.helpscout.helpscoutmock")

def search_conversations_provider_rejected(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_conversations_provider_rejected, '__module__', "dex_connectors.helpscout.helpscoutmock")

def search_conversations_invalid_response(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_conversations_invalid_response, '__module__', "dex_connectors.helpscout.helpscoutmock")

def search_conversations_defect(value: SearchConversationsOutput, failure: Failure | None = None) -> Case[SearchConversationsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_conversations_defect, '__module__', "dex_connectors.helpscout.helpscoutmock")

def search_conversations_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchConversationsOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_conversations_retry, '__module__', "dex_connectors.helpscout.helpscoutmock")

def search_conversations_searched_default_answer() -> Case[SearchConversationsOutput]:
    return Case.paged(SEARCHED, (SearchConversationsOutput(conversations=(Conversation(id=502, number=13, subject='Earlier billing question', status='active', mailbox_id=123, tags=()), Conversation(id=501, number=12, subject='Double charge on order 88213', status='active', mailbox_id=123, thread_count=1, tags=('billing',))), page=1, next_page=2, total_pages=3, total_conversations=3), SearchConversationsOutput(conversations=(Conversation(id=501, number=12, subject='Double charge on order 88213', status='active', mailbox_id=123, thread_count=1, tags=('billing',)),), page=2, next_page=3, total_pages=3, total_conversations=3), SearchConversationsOutput(conversations=(Conversation(id=503, number=14, subject='Shipping question', status='active', mailbox_id=123, tags=()),), page=3, next_page=0, total_pages=3, total_conversations=3)))

setattr(search_conversations_searched_default_answer, '__module__', "dex_connectors.helpscout.helpscoutmock")

def search_conversations_provider_rejected_failure() -> Case[SearchConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchConversationsOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "searchConversations", "Help Scout rejected the search filter (HTTP 400)."))

setattr(search_conversations_provider_rejected_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def search_conversations_invalid_response_failure() -> Case[SearchConversationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, SearchConversationsOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "searchConversations", "Help Scout returned a malformed or oversized page."))

setattr(search_conversations_invalid_response_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_conversation_found, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_merged(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(MERGED, value, failure)

setattr(get_conversation_merged, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_not_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_conversation_not_found, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_provider_rejected(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_conversation_provider_rejected, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_invalid_response(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_conversation_invalid_response, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_defect(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_conversation_defect, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[ConversationDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_conversation_retry, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_found_default_answer() -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, ConversationDetails(conversation=Conversation(id=501, number=12, subject='Double charge on order 88213', status='active', state='published', type='email', mailbox_id=123, thread_count=1, primary_customer=Person(id=238604, type='customer', email='jane@acme.example.com'), tags=('billing',)), threads=(Thread(id=560, type='customer', status='active', state='published', body='I was charged twice', created_at='2026-09-30T07:59:00Z'),), has_older_threads=False))

setattr(get_conversation_found_default_answer, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_merged_failure() -> Case[ConversationDetails]:
    return Case.branch_result(MERGED, ConversationDetails(conversation=Conversation(status=''), threads=(), merged_into_conversation_id=777), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getConversation", "Conversation 501 was merged into conversation 777."))

setattr(get_conversation_merged_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_not_found_failure() -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, ConversationDetails(conversation=Conversation(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getConversation", "The conversation does not exist, was deleted, or is not visible to the app's Help Scout user."))

setattr(get_conversation_not_found_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_provider_rejected_failure() -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, ConversationDetails(conversation=Conversation(status='')), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getConversation", "Help Scout denied conversation access (HTTP 403)."))

setattr(get_conversation_provider_rejected_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def get_conversation_invalid_response_failure() -> Case[ConversationDetails]:
    return Case.branch_result(INVALID_RESPONSE, ConversationDetails(conversation=Conversation(status='')), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getConversation", "Help Scout returned a malformed or oversized conversation or thread page."))

setattr(get_conversation_invalid_response_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def reply_to_conversation_replied(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(REPLIED, value, failure)

setattr(reply_to_conversation_replied, '__module__', "dex_connectors.helpscout.helpscoutmock")

def reply_to_conversation_not_found(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(reply_to_conversation_not_found, '__module__', "dex_connectors.helpscout.helpscoutmock")

def reply_to_conversation_provider_rejected(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(reply_to_conversation_provider_rejected, '__module__', "dex_connectors.helpscout.helpscoutmock")

def reply_to_conversation_uncertain(value: ConversationReply, failure: Failure) -> Case[ConversationReply]:
    return Case.uncertain(value, failure)

setattr(reply_to_conversation_uncertain, '__module__', "dex_connectors.helpscout.helpscoutmock")

def reply_to_conversation_defect(value: ConversationReply, failure: Failure | None = None) -> Case[ConversationReply]:
    return Case.branch_result(DEFECT, value, failure)

setattr(reply_to_conversation_defect, '__module__', "dex_connectors.helpscout.helpscoutmock")

def reply_to_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[ConversationReply]:
    return Case.retry(failure, after_seconds)

setattr(reply_to_conversation_retry, '__module__', "dex_connectors.helpscout.helpscoutmock")

def reply_to_conversation_replied_default_answer() -> Case[ConversationReply]:
    return Case.branch_result(REPLIED, ConversationReply(conversation_id=501, thread_id=567, is_internal_note=True))

setattr(reply_to_conversation_replied_default_answer, '__module__', "dex_connectors.helpscout.helpscoutmock")

def reply_to_conversation_not_found_failure() -> Case[ConversationReply]:
    return Case.branch_result(NOT_FOUND, ConversationReply(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "replyToConversation", "The conversation does not exist, was merged, or is not visible to the app's Help Scout user; nothing was added."))

setattr(reply_to_conversation_not_found_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def reply_to_conversation_provider_rejected_failure() -> Case[ConversationReply]:
    return Case.branch_result(PROVIDER_REJECTED, ConversationReply(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "replyToConversation", "Help Scout conclusively rejected the thread, such as a locked conversation with 100 threads or an unknown customer; nothing was added."))

setattr(reply_to_conversation_provider_rejected_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_updated(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_conversation_updated, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_not_found(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_conversation_not_found, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_provider_rejected(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_conversation_provider_rejected, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_invalid_response(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_conversation_invalid_response, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_defect(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_conversation_defect, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateConversationOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_conversation_retry, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_updated_default_answer() -> Case[UpdateConversationOutput]:
    return Case.branch_result(UPDATED, UpdateConversationOutput(conversation=Conversation(id=501, number=12, subject='Double charge on order 88213', status='active', state='published', type='email', mailbox_id=123, thread_count=1, primary_customer=Person(id=238604, type='customer', email='jane@acme.example.com'), tags=('billing', 'dex-triaged', 'dex-repeat-contact')), was_already_applied=False))

setattr(update_conversation_updated_default_answer, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_not_found_failure() -> Case[UpdateConversationOutput]:
    return Case.branch_result(NOT_FOUND, UpdateConversationOutput(conversation=Conversation(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateConversation", "The conversation does not exist, was merged, or is not visible to the app's Help Scout user."))

setattr(update_conversation_not_found_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_provider_rejected_failure() -> Case[UpdateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateConversationOutput(conversation=Conversation(status='')), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "updateConversation", "Help Scout refused to change the locked conversation (HTTP 412)."))

setattr(update_conversation_provider_rejected_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def update_conversation_invalid_response_failure() -> Case[UpdateConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateConversationOutput(conversation=Conversation(status='')), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "updateConversation", "Help Scout returned an oversized conversation read-back."))

setattr(update_conversation_invalid_response_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_found(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_customer_by_email_found, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_not_found(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_customer_by_email_not_found, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_provider_rejected(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_customer_by_email_provider_rejected, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_invalid_response(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_customer_by_email_invalid_response, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_defect(value: CustomerMatches, failure: Failure | None = None) -> Case[CustomerMatches]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_customer_by_email_defect, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[CustomerMatches]:
    return Case.retry(failure, after_seconds)

setattr(find_customer_by_email_retry, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_found_default_answer() -> Case[CustomerMatches]:
    return Case.branch_result(FOUND, CustomerMatches(email='jane@acme.example.com', customers=(Customer(id=238604, first_name='Jane', last_name='Smith', emails=('jane@acme.example.com',), conversation_count=3, created_at='2026-01-01T00:00:00Z'),)))

setattr(find_customer_by_email_found_default_answer, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_not_found_failure() -> Case[CustomerMatches]:
    return Case.branch_result(NOT_FOUND, CustomerMatches(email='missing@acme.example.com', customers=()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "findCustomerByEmail", "No customer profile has the email address."))

setattr(find_customer_by_email_not_found_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_provider_rejected_failure() -> Case[CustomerMatches]:
    return Case.branch_result(PROVIDER_REJECTED, CustomerMatches(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findCustomerByEmail", "Help Scout denied customer access (HTTP 403)."))

setattr(find_customer_by_email_provider_rejected_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")

def find_customer_by_email_invalid_response_failure() -> Case[CustomerMatches]:
    return Case.branch_result(INVALID_RESPONSE, CustomerMatches(), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "findCustomerByEmail", "Help Scout returned an oversized customer lookup page."))

setattr(find_customer_by_email_invalid_response_failure, '__module__', "dex_connectors.helpscout.helpscoutmock")


class Mock:

    def __init__(self, connection_name: str = "helpscout") -> None:
        self.search_conversations_script: Query[SearchConversationsInput, SearchConversationsOutput] = Query(SEARCH_CONVERSATIONS_DEFINITION)
        self.search_conversations_script.default(search_conversations_searched_default_answer())
        self.get_conversation_script: Query[GetConversationInput, ConversationDetails] = Query(GET_CONVERSATION_DEFINITION)
        self.get_conversation_script.default(get_conversation_found_default_answer())
        self.reply_to_conversation_script: Mutation[ReplyToConversationInput, ConversationReply] = Mutation(REPLY_TO_CONVERSATION_DEFINITION)
        self.reply_to_conversation_script.default(reply_to_conversation_replied_default_answer())
        self.update_conversation_script: Mutation[UpdateConversationInput, UpdateConversationOutput] = Mutation(UPDATE_CONVERSATION_DEFINITION)
        self.update_conversation_script.default(update_conversation_updated_default_answer())
        self.find_customer_by_email_script: Query[FindCustomerByEmailInput, CustomerMatches] = Query(FIND_CUSTOMER_BY_EMAIL_DEFINITION)
        self.find_customer_by_email_script.default(find_customer_by_email_found_default_answer())
        self._connection = new_connection_with_operations(
            search_conversations=self.search_conversations_script,
            get_conversation=self.get_conversation_script,
            reply_to_conversation=self.reply_to_conversation_script,
            update_conversation=self.update_conversation_script,
            find_customer_by_email=self.find_customer_by_email_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.helpscout.helpscoutmock")

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
    "get_conversation_merged",
    "get_conversation_not_found",
    "get_conversation_provider_rejected",
    "get_conversation_invalid_response",
    "get_conversation_defect",
    "get_conversation_retry",
    "get_conversation_found_default_answer",
    "get_conversation_merged_failure",
    "get_conversation_not_found_failure",
    "get_conversation_provider_rejected_failure",
    "get_conversation_invalid_response_failure",
    "reply_to_conversation_replied",
    "reply_to_conversation_not_found",
    "reply_to_conversation_provider_rejected",
    "reply_to_conversation_uncertain",
    "reply_to_conversation_defect",
    "reply_to_conversation_retry",
    "reply_to_conversation_replied_default_answer",
    "reply_to_conversation_not_found_failure",
    "reply_to_conversation_provider_rejected_failure",
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
    "find_customer_by_email_found",
    "find_customer_by_email_not_found",
    "find_customer_by_email_provider_rejected",
    "find_customer_by_email_invalid_response",
    "find_customer_by_email_defect",
    "find_customer_by_email_retry",
    "find_customer_by_email_found_default_answer",
    "find_customer_by_email_not_found_failure",
    "find_customer_by_email_provider_rejected_failure",
    "find_customer_by_email_invalid_response_failure",
]
