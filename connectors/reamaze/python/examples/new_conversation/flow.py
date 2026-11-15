# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per new Re:amaze conversation that is not spam: record it, read it back, and summarize it."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.reamaze import (
    Connection,
    ConversationCreatedEvent,
    ConversationDetails,
    GetConversationInput,
    define_conversation_created_trigger_binding,
    new_get_conversation_step,
)

CONNECTION_NAME = "reamaze-brand"
BINDING_NAME = "new-conversations"
FLOW_TYPE = "ReamazeNewConversationPython"

# Re:amaze statuses 3 (Spam) and 9 (Spam identified by AI).
SPAM_STATUSES = frozenset({3, 9})


@dataclass(frozen=True, slots=True)
class NewConversationInput:
    """One conversationCreated delivery: the stable Trigger event ID beside the delivered event."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    created: ConversationCreatedEvent


@dataclass(frozen=True, slots=True)
class Intake:
    """The Flow result: the conversation as read back from Re:amaze."""

    conversation_id: str = field(metadata={"dex_json_name": "conversationId"})
    channel: str
    status: int
    message_count: int = field(metadata={"dex_json_name": "messageCount"})
    subject: str = field(default="", metadata={"dex_json_omit_default": True})
    requester_email: str = field(
        default="", metadata={"dex_json_name": "requesterEmail", "dex_json_omit_default": True}
    )


new_conversation_attribute = dex.Attribute("reamaze-new-conversation", NewConversationInput)
intake_attribute = dex.Attribute("reamaze-new-conversation-intake", Intake)


def resolve_flow_id(event: TriggerEvent[ConversationCreatedEvent]) -> str:
    """Name one Flow per conversation, so a redelivered conversation finds its existing Flow."""
    return f"reamaze-new-conversation-{event.id}"


def map_to_flow_input(event: TriggerEvent[ConversationCreatedEvent]) -> NewConversationInput:
    """Keep the Trigger event ID beside the delivered conversation."""
    return NewConversationInput(event.id, event.payload)


def accept_customer_conversation(event: TriggerEvent[ConversationCreatedEvent]) -> bool:
    """Start no Flow for spam; the Trigger lists every view, so archived conversations still arrive."""
    return event.payload.conversation.status not in SPAM_STATUSES


def map_to_get_conversation_input(input: NewConversationInput) -> GetConversationInput:
    """Read the new conversation by its slug."""
    return GetConversationInput(conversation_id=input.created.conversation.id)


def new_intake(details: ConversationDetails) -> Intake:
    """Summarize a conversation read back from Re:amaze."""
    conversation = details.conversation
    return Intake(
        conversation_id=conversation.id,
        channel=conversation.channel.slug,
        status=conversation.status,
        message_count=len(details.messages),
        subject=conversation.subject,
        requester_email=conversation.requester.email if conversation.requester is not None else "",
    )


def validate_new_conversation_input(input: NewConversationInput) -> str:
    """Return why a delivery is unusable, or "" when it is valid."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.created.brand.strip() or not input.created.conversation.id.strip():
        return "created.brand and created.conversation.id are required"
    return ""


# dex:group group-id:intake group-label:"Intake"
# dex:explanation text:"Record the intake summary from the conversation read back and complete the Flow."
class CompleteNewConversation(dex.Step[QueryResult[ConversationDetails]]):
    """Record the intake and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteNewConversation"

    def execute(self, context: dex.Context, input: QueryResult[ConversationDetails]) -> dex.StepDecision:
        """Summarize the conversation Re:amaze returned."""
        intake = new_intake(input.value)
        intake_attribute.set(context, intake)
        return dex.graceful_complete(intake)


# dex:group group-id:intake group-label:"Intake"
# dex:explanation text:"Record the delivered conversation, then read it back from Re:amaze."
class RecordNewConversation(dex.Step[NewConversationInput]):
    """Validate and record the delivered conversation."""

    def __init__(self, read_new_conversation: dex.StepTarget[NewConversationInput]) -> None:
        """Bind the read-back Step this Step continues to."""
        self.read_new_conversation = read_new_conversation

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordNewConversation"

    def execute(self, context: dex.Context, input: NewConversationInput) -> dex.StepDecision:
        """Fail an unusable delivery; record anything else and read it back."""
        problem = validate_new_conversation_input(input)
        if problem:
            return dex.force_fail(problem)
        new_conversation_attribute.set(context, input)
        return dex.go_to(self.read_new_conversation, input)


class NewConversationFlow(dex.Flow[NewConversationInput]):
    """Record one new Re:amaze conversation and read it back with its newest messages."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Re:amaze connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"new conversation Flow requires the {CONNECTION_NAME} connection")
        self.complete = CompleteNewConversation()
        self.read_new_conversation = new_get_conversation_step(
            step_type="ReadNewConversation",
            step_input_type=NewConversationInput,
            annotations={
                "groupId": "intake",
                "groupLabel": "Intake",
                "explanation": "Read the new conversation back with its newest messages, so the intake "
                "reflects its current state.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_conversation_input,
            found=self.complete,
        )
        self.record = RecordNewConversation(self.read_new_conversation)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "ReamazeNewConversationPython"

    def get_steps(self) -> dex.StepList[NewConversationInput]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record).other_steps(self.read_new_conversation, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the delivery and intake Attributes."""
        return dex.PersistenceSchema.of(new_conversation_attribute, intake_attribute)

    # Dex Web shows the channel slug entry; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_conversation_created_trigger_binding(
                connection_name="reamaze-brand",
                binding_name="new-conversations",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="watchedChannel",
                            unit_id="channelSlugEntry",
                            label="Watched channel",
                            description=(
                                "Enter the slug of the Re:amaze channel whose new conversations start "
                                "this Flow, such as support; find it in the slug field of GET "
                                "https://{brand}.reamaze.io/api/v1/channels or under Settings > "
                                "Channels in Re:amaze. Leave it blank to start a Flow for new "
                                "conversations in every channel of the brand. The first start records "
                                "existing conversations as a baseline and starts no Flow for them. "
                                "Restart the Worker after saving; a changed channel records a new "
                                "baseline."
                            ),
                            required=False,
                            bindings=(ConnectorUIBinding(port="channel", json_pointer="/channel"),),
                        ),
                    )
                ),
            )
        ]
