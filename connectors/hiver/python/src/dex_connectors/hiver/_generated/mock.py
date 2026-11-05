# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddNoteInput, AddNoteOutput, Conversation, ConversationAssignee, ConversationDetails, ConversationMessage, CreateSharedDraftInput, GetConversationInput, Inbox, ListConversationsInput, ListConversationsOutput, ListInboxesInput, ListInboxesOutput, Note, SharedDraft, Tag, UpdateConversationInput, UpdateConversationOutput
from .definitions import CONNECTOR_ID, ADDED, ADD_NOTE_DEFINITION, CREATED, CREATE_SHARED_DRAFT_DEFINITION, DEFECT, FOUND, GET_CONVERSATION_DEFINITION, INVALID_RESPONSE, LISTED, LIST_CONVERSATIONS_DEFINITION, LIST_INBOXES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UNCERTAIN, UPDATED, UPDATE_CONVERSATION_DEFINITION

def list_inboxes_listed(value: ListInboxesOutput, failure: Failure | None = None) -> Case[ListInboxesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_inboxes_listed, '__module__', "dex_connectors.hiver.hivermock")

def list_inboxes_provider_rejected(value: ListInboxesOutput, failure: Failure | None = None) -> Case[ListInboxesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_inboxes_provider_rejected, '__module__', "dex_connectors.hiver.hivermock")

def list_inboxes_invalid_response(value: ListInboxesOutput, failure: Failure | None = None) -> Case[ListInboxesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_inboxes_invalid_response, '__module__', "dex_connectors.hiver.hivermock")

def list_inboxes_defect(value: ListInboxesOutput, failure: Failure | None = None) -> Case[ListInboxesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_inboxes_defect, '__module__', "dex_connectors.hiver.hivermock")

def list_inboxes_retry(failure: Failure, after_seconds: float = 0) -> Case[ListInboxesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_inboxes_retry, '__module__', "dex_connectors.hiver.hivermock")

def list_inboxes_listed_default_answer() -> Case[ListInboxesOutput]:
    return Case.paged(LISTED, (ListInboxesOutput(inboxes=(Inbox(id='201', display_name='Billing', email='billing@acme.example.com', channel_type='email', is_authorized=True),), next_page_token='inboxes-2'), ListInboxesOutput(inboxes=(Inbox(id='201', display_name='Billing', email='billing@acme.example.com', channel_type='email', is_authorized=True), Inbox(id='202', display_name='Sales', email='sales@acme.example.com', channel_type='email', is_authorized=True)), next_page_token='inboxes-3'), ListInboxesOutput(inboxes=(Inbox(id='105902', display_name='Support', email='support@acme.example.com', channel_type='email', is_authorized=True),))))

setattr(list_inboxes_listed_default_answer, '__module__', "dex_connectors.hiver.hivermock")

def list_inboxes_provider_rejected_failure() -> Case[ListInboxesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListInboxesOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listInboxes", "Hiver rejected the request (HTTP 401)."))

setattr(list_inboxes_provider_rejected_failure, '__module__', "dex_connectors.hiver.hivermock")

def list_inboxes_invalid_response_failure() -> Case[ListInboxesOutput]:
    return Case.branch_result(INVALID_RESPONSE, ListInboxesOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listInboxes", "Hiver returned malformed inbox JSON."))

setattr(list_inboxes_invalid_response_failure, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_listed(value: ListConversationsOutput, failure: Failure | None = None) -> Case[ListConversationsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_conversations_listed, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_not_found(value: ListConversationsOutput, failure: Failure | None = None) -> Case[ListConversationsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_conversations_not_found, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_provider_rejected(value: ListConversationsOutput, failure: Failure | None = None) -> Case[ListConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_conversations_provider_rejected, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_invalid_response(value: ListConversationsOutput, failure: Failure | None = None) -> Case[ListConversationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_conversations_invalid_response, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_defect(value: ListConversationsOutput, failure: Failure | None = None) -> Case[ListConversationsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_conversations_defect, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_retry(failure: Failure, after_seconds: float = 0) -> Case[ListConversationsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_conversations_retry, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_listed_default_answer() -> Case[ListConversationsOutput]:
    return Case.paged(LISTED, (ListConversationsOutput(conversations=(Conversation(id='573741350', inbox_id='105902', status='closed', tag_ids=()),), next_page_token='conversations-2'), ListConversationsOutput(conversations=(Conversation(id='573741350', inbox_id='105902', status='closed', tag_ids=()), Conversation(id='573741351', inbox_id='105902', status='open', assignee=ConversationAssignee(type='user', id='456343'), tag_ids=())), next_page_token='conversations-3'), ListConversationsOutput(conversations=(Conversation(id='573741352', inbox_id='105902', status='open', tag_ids=()),))))

setattr(list_conversations_listed_default_answer, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_not_found_failure() -> Case[ListConversationsOutput]:
    return Case.branch_result(NOT_FOUND, ListConversationsOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listConversations", "The shared inbox does not exist or is not visible to the API key."))

setattr(list_conversations_not_found_failure, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_provider_rejected_failure() -> Case[ListConversationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListConversationsOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listConversations", "Hiver rejected the request (HTTP 401)."))

setattr(list_conversations_provider_rejected_failure, '__module__', "dex_connectors.hiver.hivermock")

def list_conversations_invalid_response_failure() -> Case[ListConversationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, ListConversationsOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listConversations", "Hiver returned malformed conversation-page JSON."))

setattr(list_conversations_invalid_response_failure, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_conversation_found, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_not_found(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_conversation_not_found, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_provider_rejected(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_conversation_provider_rejected, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_invalid_response(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_conversation_invalid_response, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_defect(value: ConversationDetails, failure: Failure | None = None) -> Case[ConversationDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_conversation_defect, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[ConversationDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_conversation_retry, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_found_default_answer() -> Case[ConversationDetails]:
    return Case.branch_result(FOUND, ConversationDetails(conversation=Conversation(id='573741352', inbox_id='105902', status='open', tag_ids=()), messages=(ConversationMessage(hiver_message_id='834466048', gmail_message_id='19cfee91188070f8'),)))

setattr(get_conversation_found_default_answer, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_not_found_failure() -> Case[ConversationDetails]:
    return Case.branch_result(NOT_FOUND, ConversationDetails(conversation=Conversation(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getConversation", "The conversation or shared inbox does not exist or is not visible to the API key."))

setattr(get_conversation_not_found_failure, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_provider_rejected_failure() -> Case[ConversationDetails]:
    return Case.branch_result(PROVIDER_REJECTED, ConversationDetails(conversation=Conversation(status='')), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getConversation", "Hiver rejected the request (HTTP 401)."))

setattr(get_conversation_provider_rejected_failure, '__module__', "dex_connectors.hiver.hivermock")

def get_conversation_invalid_response_failure() -> Case[ConversationDetails]:
    return Case.branch_result(INVALID_RESPONSE, ConversationDetails(conversation=Conversation(status='')), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getConversation", "Hiver returned malformed conversation JSON."))

setattr(get_conversation_invalid_response_failure, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_updated(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_conversation_updated, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_not_found(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_conversation_not_found, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_provider_rejected(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_conversation_provider_rejected, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_invalid_response(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_conversation_invalid_response, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_defect(value: UpdateConversationOutput, failure: Failure | None = None) -> Case[UpdateConversationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_conversation_defect, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateConversationOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_conversation_retry, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_updated_default_answer() -> Case[UpdateConversationOutput]:
    return Case.branch_result(UPDATED, UpdateConversationOutput(conversation=Conversation(id='573741352', inbox_id='105902', status='open', assignee=ConversationAssignee(type='user', id='456342'), tag_ids=('784268',)), assignee_user_id='456342', applied_tags=(Tag(id='784268', name='Claimed'),)))

setattr(update_conversation_updated_default_answer, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_not_found_failure() -> Case[UpdateConversationOutput]:
    return Case.branch_result(NOT_FOUND, UpdateConversationOutput(conversation=Conversation(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateConversation", "The conversation or shared inbox does not exist or is not visible to the API key."))

setattr(update_conversation_not_found_failure, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_provider_rejected_failure() -> Case[UpdateConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateConversationOutput(conversation=Conversation(status='')), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updateConversation", "assigneeEmail matches 0 users of the shared inbox instead of one; the change was not sent"))

setattr(update_conversation_provider_rejected_failure, '__module__', "dex_connectors.hiver.hivermock")

def update_conversation_invalid_response_failure() -> Case[UpdateConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateConversationOutput(conversation=Conversation(status='')), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "updateConversation", "Hiver returned malformed conversation JSON during read-back after accepting the change."))

setattr(update_conversation_invalid_response_failure, '__module__', "dex_connectors.hiver.hivermock")

def add_note_added(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_note_added, '__module__', "dex_connectors.hiver.hivermock")

def add_note_not_found(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_note_not_found, '__module__', "dex_connectors.hiver.hivermock")

def add_note_provider_rejected(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_note_provider_rejected, '__module__', "dex_connectors.hiver.hivermock")

def add_note_uncertain(value: AddNoteOutput, failure: Failure) -> Case[AddNoteOutput]:
    return Case.uncertain(value, failure)

setattr(add_note_uncertain, '__module__', "dex_connectors.hiver.hivermock")

def add_note_defect(value: AddNoteOutput, failure: Failure | None = None) -> Case[AddNoteOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_note_defect, '__module__', "dex_connectors.hiver.hivermock")

def add_note_retry(failure: Failure, after_seconds: float = 0) -> Case[AddNoteOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_note_retry, '__module__', "dex_connectors.hiver.hivermock")

def add_note_added_default_answer() -> Case[AddNoteOutput]:
    return Case.branch_result(ADDED, AddNoteOutput(note=Note(id='35042650', conversation_id='573741352', author_id='456342', author_email='phoebe@acme.example.com', created_at=datetime.fromisoformat("2026-07-21T14:24:22Z".replace('Z', '+00:00')))))

setattr(add_note_added_default_answer, '__module__', "dex_connectors.hiver.hivermock")

def add_note_not_found_failure() -> Case[AddNoteOutput]:
    return Case.branch_result(NOT_FOUND, AddNoteOutput(note=Note()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addNote", "The conversation or shared inbox does not exist or is not visible to the API key; nothing was added."))

setattr(add_note_not_found_failure, '__module__', "dex_connectors.hiver.hivermock")

def add_note_provider_rejected_failure() -> Case[AddNoteOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddNoteOutput(note=Note()), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "addNote", "Hiver rejected the request (HTTP 400)."))

setattr(add_note_provider_rejected_failure, '__module__', "dex_connectors.hiver.hivermock")

def create_shared_draft_created(value: SharedDraft, failure: Failure | None = None) -> Case[SharedDraft]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_shared_draft_created, '__module__', "dex_connectors.hiver.hivermock")

def create_shared_draft_not_found(value: SharedDraft, failure: Failure | None = None) -> Case[SharedDraft]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(create_shared_draft_not_found, '__module__', "dex_connectors.hiver.hivermock")

def create_shared_draft_provider_rejected(value: SharedDraft, failure: Failure | None = None) -> Case[SharedDraft]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_shared_draft_provider_rejected, '__module__', "dex_connectors.hiver.hivermock")

def create_shared_draft_uncertain(value: SharedDraft, failure: Failure) -> Case[SharedDraft]:
    return Case.uncertain(value, failure)

setattr(create_shared_draft_uncertain, '__module__', "dex_connectors.hiver.hivermock")

def create_shared_draft_defect(value: SharedDraft, failure: Failure | None = None) -> Case[SharedDraft]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_shared_draft_defect, '__module__', "dex_connectors.hiver.hivermock")

def create_shared_draft_retry(failure: Failure, after_seconds: float = 0) -> Case[SharedDraft]:
    return Case.retry(failure, after_seconds)

setattr(create_shared_draft_retry, '__module__', "dex_connectors.hiver.hivermock")

def create_shared_draft_created_default_answer() -> Case[SharedDraft]:
    return Case.branch_result(CREATED, SharedDraft(id='901', conversation_id='573741352', reply_to_hiver_message_id='834466048'))

setattr(create_shared_draft_created_default_answer, '__module__', "dex_connectors.hiver.hivermock")

def create_shared_draft_not_found_failure() -> Case[SharedDraft]:
    return Case.branch_result(NOT_FOUND, SharedDraft(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "createSharedDraft", "The message or shared inbox does not exist or is not visible to the API key; no draft was created."))

setattr(create_shared_draft_not_found_failure, '__module__', "dex_connectors.hiver.hivermock")

def create_shared_draft_provider_rejected_failure() -> Case[SharedDraft]:
    return Case.branch_result(PROVIDER_REJECTED, SharedDraft(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createSharedDraft", "Hiver rejected the request (HTTP 400)."))

setattr(create_shared_draft_provider_rejected_failure, '__module__', "dex_connectors.hiver.hivermock")


class Mock:

    def __init__(self, connection_name: str = "hiver") -> None:
        self.list_inboxes_script: Query[ListInboxesInput, ListInboxesOutput] = Query(LIST_INBOXES_DEFINITION)
        self.list_inboxes_script.default(list_inboxes_listed_default_answer())
        self.list_conversations_script: Query[ListConversationsInput, ListConversationsOutput] = Query(LIST_CONVERSATIONS_DEFINITION)
        self.list_conversations_script.default(list_conversations_listed_default_answer())
        self.get_conversation_script: Query[GetConversationInput, ConversationDetails] = Query(GET_CONVERSATION_DEFINITION)
        self.get_conversation_script.default(get_conversation_found_default_answer())
        self.update_conversation_script: Mutation[UpdateConversationInput, UpdateConversationOutput] = Mutation(UPDATE_CONVERSATION_DEFINITION)
        self.update_conversation_script.default(update_conversation_updated_default_answer())
        self.add_note_script: Mutation[AddNoteInput, AddNoteOutput] = Mutation(ADD_NOTE_DEFINITION)
        self.add_note_script.default(add_note_added_default_answer())
        self.create_shared_draft_script: Mutation[CreateSharedDraftInput, SharedDraft] = Mutation(CREATE_SHARED_DRAFT_DEFINITION)
        self.create_shared_draft_script.default(create_shared_draft_created_default_answer())
        self._connection = new_connection_with_operations(
            list_inboxes=self.list_inboxes_script,
            list_conversations=self.list_conversations_script,
            get_conversation=self.get_conversation_script,
            update_conversation=self.update_conversation_script,
            add_note=self.add_note_script,
            create_shared_draft=self.create_shared_draft_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.hiver.hivermock")

__all__ = [
    "Mock",
    "list_inboxes_listed",
    "list_inboxes_provider_rejected",
    "list_inboxes_invalid_response",
    "list_inboxes_defect",
    "list_inboxes_retry",
    "list_inboxes_listed_default_answer",
    "list_inboxes_provider_rejected_failure",
    "list_inboxes_invalid_response_failure",
    "list_conversations_listed",
    "list_conversations_not_found",
    "list_conversations_provider_rejected",
    "list_conversations_invalid_response",
    "list_conversations_defect",
    "list_conversations_retry",
    "list_conversations_listed_default_answer",
    "list_conversations_not_found_failure",
    "list_conversations_provider_rejected_failure",
    "list_conversations_invalid_response_failure",
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
    "add_note_added",
    "add_note_not_found",
    "add_note_provider_rejected",
    "add_note_uncertain",
    "add_note_defect",
    "add_note_retry",
    "add_note_added_default_answer",
    "add_note_not_found_failure",
    "add_note_provider_rejected_failure",
    "create_shared_draft_created",
    "create_shared_draft_not_found",
    "create_shared_draft_provider_rejected",
    "create_shared_draft_uncertain",
    "create_shared_draft_defect",
    "create_shared_draft_retry",
    "create_shared_draft_created_default_answer",
    "create_shared_draft_not_found_failure",
    "create_shared_draft_provider_rejected_failure",
]
