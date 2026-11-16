# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Flow per new Help Scout conversation, read it back, and tag it ``dex-triaged``."""

from dataclasses import dataclass, field, replace

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    MutationResult,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.helpscout import (
    WEBHOOK_EVENT_CONVERSATION_CREATED,
    Connection,
    ConversationDetails,
    ConversationEvent,
    GetConversationInput,
    UpdateConversationInput,
    UpdateConversationOutput,
    define_conversation_event_trigger_binding,
)
from dex_connectors.helpscout.steps import new_get_conversation_step, new_update_conversation_step

CONNECTION_NAME = "helpscout-support"
NEW_CONVERSATION_TRIGGER_BINDING = "new-conversations"
FLOW_ID_PREFIX = "helpscout-"
TRIAGED_TAG = "dex-triaged"


@dataclass(frozen=True, slots=True)
class NewConversationInput:
    """Flow start input mapped from one conversation webhook: identities only, never message bodies."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    event: str
    conversation_id: int = field(metadata={"dex_json_name": "conversationId"})
    mailbox_id: int = field(metadata={"dex_json_name": "mailboxId"})


@dataclass(frozen=True, slots=True)
class TaggingRecord:
    """The durable tagging record and the Flow result."""

    request: NewConversationInput
    stage: str
    status: str = ""
    newest_thread_types: tuple[str, ...] = field(default=(), metadata={"dex_json_name": "newestThreadTypes"})
    tags: tuple[str, ...] = ()


tagging_record = dex.Attribute("helpscout-new-conversation-tagging", TaggingRecord)


def accept_new_conversation(event: TriggerEvent[ConversationEvent]) -> bool:
    """Admit only a ``convo.created`` event for an active conversation."""
    return (
        event.payload.event == WEBHOOK_EVENT_CONVERSATION_CREATED
        and event.payload.conversation.status == "active"
    )


def resolve_flow_id(event: TriggerEvent[ConversationEvent]) -> str:
    """Build the Flow ID from the Trigger event ID, so a redelivery of the same body maps to one Flow."""
    return FLOW_ID_PREFIX + event.id.replace(":", "-")


def map_to_flow_input(event: TriggerEvent[ConversationEvent]) -> NewConversationInput:
    """Map one conversation webhook to the Flow input."""
    conversation = event.payload.conversation
    return NewConversationInput(event.id, event.payload.event, conversation.id, conversation.mailbox_id)


# dex:group group-id:tagging group-label:"Tagging"
# dex:explanation text:"Store the Help Scout webhook identities before reading the conversation."
class RecordNewConversation(dex.Step[NewConversationInput]):
    """Store the request before reading the conversation."""

    def get_step_type(self) -> str:
        """Return the stable Step type."""
        return "RecordNewHelpScoutConversation"

    def execute(self, context: dex.Context, input: NewConversationInput) -> dex.StepDecision:
        """Record the request and read the conversation."""
        tagging_record.set(context, TaggingRecord(input, "reading"))
        return dex.go_to(dex.StepRef("ReadHelpScoutConversation"), input)


# dex:group group-id:tagging group-label:"Tagging"
# dex:explanation text:"Record the conversation status and newest thread types before tagging it."
class InspectConversation(dex.Step[QueryResult[ConversationDetails]]):
    """Record the status and newest thread types before tagging."""

    def get_step_type(self) -> str:
        """Return the stable Step type."""
        return "InspectHelpScoutConversation"

    def execute(self, context: dex.Context, input: QueryResult[ConversationDetails]) -> dex.StepDecision:
        """Record what was read and tag the conversation."""
        record = replace(
            tagging_record.get(context),
            stage="tagging",
            status=input.value.conversation.status,
            newest_thread_types=tuple(thread.type for thread in input.value.threads),
        )
        tagging_record.set(context, record)
        return dex.go_to(dex.StepRef("TagHelpScoutConversation"), record)


# dex:group group-id:tagging group-label:"Tagging"
# dex:explanation text:"Store the tags Help Scout returned and complete the Flow."
class CompleteTagging(dex.Step[MutationResult[UpdateConversationOutput]]):
    """Complete with the conversation's tags."""

    def get_step_type(self) -> str:
        """Return the stable Step type."""
        return "CompleteHelpScoutTagging"

    def execute(
        self, context: dex.Context, input: MutationResult[UpdateConversationOutput]
    ) -> dex.StepDecision:
        """Record completion and return the record."""
        record = replace(tagging_record.get(context), stage="completed", tags=input.value.conversation.tags)
        tagging_record.set(context, record)
        return dex.graceful_complete(record)


class TagNewConversationFlow(dex.Flow[NewConversationInput]):
    """A new Help Scout conversation starts the Flow, which reads it and tags it ``dex-triaged``."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Help Scout connection that the connector Steps call."""
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("the tagging Flow requires the helpscout-support connection")
        self._record = RecordNewConversation()
        self._inspect = InspectConversation()
        self._complete = CompleteTagging()
        self._read = new_get_conversation_step(
            step_type="ReadHelpScoutConversation",
            step_input_type=NewConversationInput,
            annotations={
                "groupId": "helpscout",
                "groupLabel": "Help Scout",
                "explanation": "Read the new conversation with its five newest threads.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: GetConversationInput(request.conversation_id, 5),
            found=self._inspect,
        )
        self._tag = new_update_conversation_step(
            step_type="TagHelpScoutConversation",
            step_input_type=TaggingRecord,
            annotations={
                "groupId": "helpscout",
                "groupLabel": "Help Scout",
                "explanation": "Add the dex-triaged tag; a repeat finds it applied and writes nothing.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda record: UpdateConversationInput(
                record.request.conversation_id, add_tags=(TRIAGED_TAG,)
            ),
            updated=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow type."""
        return "HelpScoutTagNewConversationPython"

    def get_steps(self) -> dex.StepList[NewConversationInput]:
        """Register the application and connector Steps; only happy-path branches are wired."""
        return dex.StepList.start_step(self._record).other_steps(
            self._read, self._inspect, self._tag, self._complete
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the tagging record."""
        return dex.PersistenceSchema.of(tagging_record)

    # Declares the binding and its inbox picker for Dex Web; the analyzer reads one static return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_conversation_event_trigger_binding(
                connection_name="helpscout-support",
                binding_name="new-conversations",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "inbox",
                            "mailboxPicker",
                            "Inbox",
                            "Select the Help Scout inbox whose new conversations start this Flow; the "
                            "picker lists the inboxes the connected user can access and stores the inbox "
                            "ID. Leave it empty to tag new conversations of every inbox.",
                            False,
                            (ConnectorUIBinding("mailboxId", "/mailboxId"),),
                        ),
                    )
                ),
            )
        ]
