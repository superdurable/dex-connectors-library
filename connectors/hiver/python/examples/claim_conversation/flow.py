# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise all six Hiver operations in one durable conversation claim Flow."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.hiver import (
    AddNoteInput,
    AddNoteOutput,
    Connection,
    ConversationDetails,
    CreateSharedDraftInput,
    GetConversationInput,
    ListConversationsInput,
    ListConversationsOutput,
    ListInboxesInput,
    ListInboxesOutput,
    SharedDraft,
    UpdateConversationInput,
    UpdateConversationOutput,
    new_add_note_step,
    new_create_shared_draft_step,
    new_get_conversation_step,
    new_list_conversations_step,
    new_list_inboxes_step,
    new_update_conversation_step,
)

CONNECTION_NAME = "hiver-support"
FLOW_TYPE = "HiverClaimConversationPython"


@dataclass(frozen=True, slots=True)
class ClaimRequest:
    """Public IDs and text for one Hiver conversation claim."""

    inbox_id: str
    conversation_id: str
    hiver_message_id: str
    note_content: str
    draft_body: str
    status: str = "pending"


@dataclass(frozen=True, slots=True)
class ClaimOutcome:
    """Credential-free IDs for the claimed conversation, note, and shared draft."""

    conversation_id: str
    status: str
    note_id: str
    shared_draft_id: str


_REQUEST = dex.Attribute("hiver-python-claim-request", ClaimRequest)
_NOTE_ID = dex.Attribute("hiver-python-claim-note-id", str)
_CONVERSATION_ID = dex.Attribute("hiver-python-claim-conversation-id", str)


def _input_list_inboxes(request: ClaimRequest) -> ListInboxesInput:
    return ListInboxesInput()


def _input_list_conversations(request: ClaimRequest) -> ListConversationsInput:
    return ListConversationsInput(inbox_id=request.inbox_id)


def _input_get_conversation(request: ClaimRequest) -> GetConversationInput:
    return GetConversationInput(request.inbox_id, request.conversation_id)


def _input_update_conversation(request: ClaimRequest) -> UpdateConversationInput:
    return UpdateConversationInput(request.inbox_id, request.conversation_id, status=request.status)


def _input_add_note(request: ClaimRequest) -> AddNoteInput:
    return AddNoteInput(request.inbox_id, request.conversation_id, request.note_content)


def _input_create_shared_draft(request: ClaimRequest) -> CreateSharedDraftInput:
    return CreateSharedDraftInput(request.inbox_id, request.hiver_message_id, body=request.draft_body)


# dex:group group-id:hiver-claim group-label:"Hiver claim"
# dex:explanation text:"Persist the request before any provider calls."
class RecordClaimRequest(dex.Step[ClaimRequest]):
    """Store the safe claim input for later provider Steps."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordHiverClaimRequest"

    def execute(self, context: dex.Context, input: ClaimRequest) -> dex.StepDecision:
        """Persist the request and begin the provider sequence."""
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("ListHiverInboxesForClaim"), input)


# dex:group group-id:hiver-claim group-label:"Hiver claim"
# dex:explanation text:"Confirm the selected shared inbox is in the returned page."
class InboxListed(dex.Step[QueryResult[ListInboxesOutput]]):
    """Continue only when the selected inbox is in the page."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "HiverClaimInboxListed"

    def execute(self, context: dex.Context, input: QueryResult[ListInboxesOutput]) -> dex.StepDecision:
        """Check the requested inbox and continue to conversation listing."""
        request = _REQUEST.get(context)
        if not any(inbox.id == request.inbox_id for inbox in input.value.inboxes):
            return dex.force_fail("selected Hiver inbox is not on the returned page")
        return dex.go_to(dex.StepRef("ListHiverConversationsForClaim"), request)


# dex:group group-id:hiver-claim group-label:"Hiver claim"
# dex:explanation text:"Confirm the selected conversation is in the returned page."
class ConversationsListed(dex.Step[QueryResult[ListConversationsOutput]]):
    """Continue only when the selected conversation is in the page."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "HiverClaimConversationsListed"

    def execute(self, context: dex.Context, input: QueryResult[ListConversationsOutput]) -> dex.StepDecision:
        """Check the requested conversation and continue to its detailed read."""
        request = _REQUEST.get(context)
        if not any(conversation.id == request.conversation_id for conversation in input.value.conversations):
            return dex.force_fail("selected Hiver conversation is not on the returned page")
        return dex.go_to(dex.StepRef("GetHiverConversationForClaim"), request)


# dex:group group-id:hiver-claim group-label:"Hiver claim"
# dex:explanation text:"Confirm the reply target belongs to the conversation before changing it."
class ConversationFound(dex.Step[QueryResult[ConversationDetails]]):
    """Check the message ID against Hiver's detailed read."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "HiverClaimConversationFound"

    def execute(self, context: dex.Context, input: QueryResult[ConversationDetails]) -> dex.StepDecision:
        """Check the reply target and continue to the absolute update."""
        request = _REQUEST.get(context)
        if not any(message.hiver_message_id == request.hiver_message_id for message in input.value.messages):
            return dex.force_fail("reply target is not a message of the Hiver conversation")
        return dex.go_to(dex.StepRef("UpdateHiverConversationForClaim"), request)


# dex:group group-id:hiver-claim group-label:"Hiver claim"
# dex:explanation text:"Use the read-back status and conversation ID after the absolute update."
class ConversationUpdated(dex.Step[MutationResult[UpdateConversationOutput]]):
    """Retain the confirmed conversation ID for completion."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "HiverClaimConversationUpdated"

    def execute(
        self, context: dex.Context, input: MutationResult[UpdateConversationOutput]
    ) -> dex.StepDecision:
        _CONVERSATION_ID.set(context, input.value.conversation.id)
        return dex.go_to(dex.StepRef("AddHiverClaimNote"), _REQUEST.get(context))


# dex:group group-id:hiver-claim group-label:"Hiver claim"
# dex:explanation text:"Retain the accepted internal note ID before creating a shared draft."
class NoteAdded(dex.Step[MutationResult[AddNoteOutput]]):
    """Retain the note ID and continue to the shared draft."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "HiverClaimNoteAdded"

    def execute(self, context: dex.Context, input: MutationResult[AddNoteOutput]) -> dex.StepDecision:
        """Keep the accepted note ID and continue to the shared draft."""
        _NOTE_ID.set(context, input.value.note.id)
        return dex.go_to(dex.StepRef("CreateHiverSharedDraftForClaim"), _REQUEST.get(context))


# dex:group group-id:hiver-claim group-label:"Hiver claim"
# dex:explanation text:"Complete with the confirmed conversation, internal note, and shared draft IDs."
class ClaimCompleted(dex.Step[MutationResult[SharedDraft]]):
    """Complete the Flow after Hiver accepts the draft."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteHiverConversationClaim"

    def execute(self, context: dex.Context, input: MutationResult[SharedDraft]) -> dex.StepDecision:
        """Complete with credential-free provider object IDs."""
        request = _REQUEST.get(context)
        return dex.graceful_complete(
            ClaimOutcome(_CONVERSATION_ID.get(context), request.status, _NOTE_ID.get(context), input.value.id)
        )


class ClaimConversationFlow(dex.Flow[ClaimRequest]):
    """Read, claim, annotate, and draft a reply through one Hiver connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Hiver claim Flow requires its static connection name")
        self.record = RecordClaimRequest()
        self.inbox_listed = InboxListed()
        self.conversations_listed = ConversationsListed()
        self.conversation_found = ConversationFound()
        self.conversation_updated = ConversationUpdated()
        self.note_added = NoteAdded()
        self.completed = ClaimCompleted()
        annotations = {
            "groupId": "hiver-claim",
            "groupLabel": "Hiver claim",
        }
        self.list_inboxes = new_list_inboxes_step(
            step_type="ListHiverInboxesForClaim",
            step_input_type=ClaimRequest,
            annotations={
                **annotations,
                "explanation": "List the shared inboxes and confirm the selected inbox.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_input_list_inboxes,
            listed=self.inbox_listed,
        )
        self.list_conversations = new_list_conversations_step(
            step_type="ListHiverConversationsForClaim",
            step_input_type=ClaimRequest,
            annotations={**annotations, "explanation": "List conversations of the selected shared inbox."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_input_list_conversations,
            listed=self.conversations_listed,
        )
        self.get_conversation = new_get_conversation_step(
            step_type="GetHiverConversationForClaim",
            step_input_type=ClaimRequest,
            annotations={
                **annotations,
                "explanation": "Read the selected conversation and confirm its reply target.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_input_get_conversation,
            found=self.conversation_found,
        )
        self.update_conversation = new_update_conversation_step(
            step_type="UpdateHiverConversationForClaim",
            step_input_type=ClaimRequest,
            annotations={**annotations, "explanation": "Set the conversation status and read it back."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_input_update_conversation,
            updated=self.conversation_updated,
        )
        self.add_note = new_add_note_step(
            step_type="AddHiverClaimNote",
            step_input_type=ClaimRequest,
            annotations={**annotations, "explanation": "Add an internal note visible to Hiver users."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_input_add_note,
            added=self.note_added,
        )
        self.create_shared_draft = new_create_shared_draft_step(
            step_type="CreateHiverSharedDraftForClaim",
            step_input_type=ClaimRequest,
            annotations={
                **annotations,
                "explanation": "Create a shared reply draft for a Hiver user to send.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_input_create_shared_draft,
            created=self.completed,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ClaimRequest]:
        """Register all provider and continuation Steps."""
        return dex.StepList.start_step(self.record).other_steps(
            self.list_inboxes,
            self.inbox_listed,
            self.list_conversations,
            self.conversations_listed,
            self.get_conversation,
            self.conversation_found,
            self.update_conversation,
            self.conversation_updated,
            self.add_note,
            self.note_added,
            self.create_shared_draft,
            self.completed,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register every Attribute consumed by the Flow."""
        return dex.PersistenceSchema.of(_REQUEST, _NOTE_ID, _CONVERSATION_ID)
