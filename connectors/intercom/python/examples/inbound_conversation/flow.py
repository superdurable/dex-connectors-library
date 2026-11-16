# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per new Intercom conversation: record the notification and read the conversation back."""

import re
from dataclasses import dataclass, field
from datetime import datetime

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.intercom import (
    TOPIC_CONVERSATION_USER_CREATED,
    Connection,
    ConversationDetails,
    ConversationEvent,
    GetConversationInput,
    define_conversation_event_trigger_binding,
    new_get_conversation_step,
)

CONNECTION_NAME = "intercom-support-inbox"
BINDING_NAME = "inbound-conversation"
FLOW_TYPE = "IntercomInboundConversationPython"
FLOW_ID_PREFIX = "intercom-inbound-conversation-"
_CONVERSATION_ID = re.compile(r"[0-9]{1,24}\Z")


@dataclass(frozen=True, slots=True)
class InboundConversation:
    """Start input mapped from one notification, or a conversation ID entered in Dex Web Start Flow."""

    conversation_id: str = field(metadata={"dex_json_name": "conversationId"})
    notification_id: str = field(default="", metadata={"dex_json_name": "notificationId"})
    topic: str = ""
    workspace_id: str = field(
        default="", metadata={"dex_json_name": "workspaceId", "dex_json_omit_default": True}
    )
    notified_at: datetime | None = field(
        default=None,
        metadata={"dex_json_name": "notifiedAt", "dex_json_omit_default": True, "dex_json_format": "rfc3339"},
    )


@dataclass(frozen=True, slots=True)
class InboundConversationOutcome:
    """The Flow result: the conversation's identity, state, and starting customer."""

    conversation_id: str = field(metadata={"dex_json_name": "conversationId"})
    notification_id: str = field(metadata={"dex_json_name": "notificationId"})
    conversation_state: str = field(metadata={"dex_json_name": "conversationState"})
    customer_email: str = field(metadata={"dex_json_name": "customerEmail"})
    part_count: int = field(metadata={"dex_json_name": "partCount"})


inbound_conversation_attribute = dex.Attribute("intercom-inbound-conversation", InboundConversation)
inbound_conversation_outcome_attribute = dex.Attribute(
    "intercom-inbound-conversation-outcome", InboundConversationOutcome
)


def accept_inbound_conversation(event: TriggerEvent[ConversationEvent]) -> bool:
    """Admit only a new conversation, whatever topics the stored binding selects."""
    return event.payload.topic == TOPIC_CONVERSATION_USER_CREATED and event.payload.conversation.id != ""


def resolve_flow_id(event: TriggerEvent[ConversationEvent]) -> str:
    """Name the Flow by the conversation ID, so a redelivery starts no second Flow."""
    return FLOW_ID_PREFIX + event.payload.conversation.id


def map_to_flow_input(event: TriggerEvent[ConversationEvent]) -> InboundConversation:
    """Copy the notification's identity into the start input; no message text enters Flow state."""
    return InboundConversation(
        conversation_id=event.payload.conversation.id,
        notification_id=event.payload.notification_id,
        topic=event.payload.topic,
        workspace_id=event.payload.workspace_id,
        notified_at=event.payload.notified_at,
    )


def map_to_get_conversation_input(input: InboundConversation) -> GetConversationInput:
    """Read the five latest parts of the inbound conversation."""
    return GetConversationInput(conversation_id=input.conversation_id, latest_part_limit=5)


def choose_inbound_conversation_outcome(
    inbound: InboundConversation, details: ConversationDetails
) -> InboundConversationOutcome:
    """Choose the outcome: the source author's email, or "" when Intercom sent none."""
    return InboundConversationOutcome(
        conversation_id=details.conversation.id,
        notification_id=inbound.notification_id,
        conversation_state=details.conversation.state,
        customer_email=details.conversation.source.author.email,
        part_count=details.part_count,
    )


def validate_inbound_conversation(input: InboundConversation) -> str:
    """Return why a start input is unusable, or "" when it names a conversation."""
    if _CONVERSATION_ID.match(input.conversation_id or "") is None:
        return "conversationId must be an Intercom conversation ID of digits"
    return ""


# dex:group group-id:inbound group-label:"Inbound"
# dex:explanation text:"Complete with the conversation state and the customer who started it."
class CompleteInboundConversation(dex.Step[QueryResult[ConversationDetails]]):
    """Build the outcome from the recorded notification and the conversation read back."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteInboundConversation"

    def execute(self, context: dex.Context, input: QueryResult[ConversationDetails]) -> dex.StepDecision:
        """Record and return the outcome."""
        outcome = choose_inbound_conversation_outcome(
            inbound_conversation_attribute.get(context), input.value
        )
        inbound_conversation_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:inbound group-label:"Inbound"
# dex:explanation text:"Record the inbound notification before reading its conversation."
class RecordInboundConversation(dex.Step[InboundConversation]):
    """Validate and record the notification."""

    def __init__(self, read_inbound_conversation: dex.StepTarget[InboundConversation]) -> None:
        """Bind the read Step this Step continues to."""
        self.read_inbound_conversation = read_inbound_conversation

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordInboundConversation"

    def execute(self, context: dex.Context, input: InboundConversation) -> dex.StepDecision:
        """Store the notification and read the conversation."""
        problem = validate_inbound_conversation(input)
        if problem:
            return dex.force_fail(problem)
        inbound_conversation_attribute.set(context, input)
        return dex.go_to(self.read_inbound_conversation, input)


class InboundConversationFlow(dex.Flow[InboundConversation]):
    """A new Intercom conversation starts this Flow, which reads it and completes with its customer."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Intercom connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"inbound conversation Flow requires the {CONNECTION_NAME} connection")
        self.complete = CompleteInboundConversation()
        self.read_inbound_conversation = new_get_conversation_step(
            step_type="ReadInboundConversation",
            step_input_type=InboundConversation,
            annotations={
                "groupId": "intercom",
                "groupLabel": "Intercom",
                "explanation": "Read the new conversation and its latest parts as plain text.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_conversation_input,
            found=self.complete,
        )
        self.record = RecordInboundConversation(self.read_inbound_conversation)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "IntercomInboundConversationPython"

    def get_steps(self) -> dex.StepList[InboundConversation]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record).other_steps(self.read_inbound_conversation, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the notification and outcome Attributes."""
        return dex.PersistenceSchema.of(
            inbound_conversation_attribute, inbound_conversation_outcome_attribute
        )

    # Declares the conversationEvent binding and its topic picker for Dex Web; Dex reads one static return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_conversation_event_trigger_binding(
                connection_name="intercom-support-inbox",
                binding_name="inbound-conversation",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "topics",
                            "conversationTopicPicker",
                            "Topics that start the Flow",
                            "Select conversation.user.created, the topic Intercom sends once for each "
                            "conversation a customer starts; subscribe the same topic on the Intercom app's "
                            "Configure > Webhooks page. The Flow itself admits only that topic, so selecting "
                            "none accepts every supported topic and still starts Flows only for new "
                            "conversations.",
                            False,
                            (ConnectorUIBinding("topics", "/topics"),),
                        ),
                    )
                ),
            )
        ]
