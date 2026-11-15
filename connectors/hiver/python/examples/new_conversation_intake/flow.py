# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per new Hiver conversation: record it, read its message IDs back, and record the intake."""

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

from dex_connectors.hiver import (
    Connection,
    ConversationDetails,
    ConversationMessage,
    ConversationReceivedEvent,
    GetConversationInput,
    define_conversation_received_trigger_binding,
    new_get_conversation_step,
)

FLOW_TYPE = "HiverNewConversationIntakePython"
CONNECTION_NAME = "hiver-support"
"""The static Dex Web connection whose shared inbox the Trigger polls."""
CONVERSATION_RECEIVED_TRIGGER_BINDING = "new-support-conversations"
"""The conversationReceived binding whose conversations start this Flow."""
FLOW_ID_PREFIX = "hiver-new-conversation-"
"""Precedes the Trigger event ID in every Flow ID, so a redelivered conversation maps to one Flow."""


@dataclass(frozen=True, slots=True)
class ReceivedConversation:
    """One conversationReceived delivery: the stable Trigger event ID beside the delivered conversation."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    received: ConversationReceivedEvent
    detected_at: datetime = field(metadata={"dex_json_name": "detectedAt"})
    """When the poll found the conversation; Hiver's list carries no timestamp."""


@dataclass(frozen=True, slots=True)
class IntakeOutcome:
    """The Flow result: the conversation's status and message IDs as Hiver reported them on read-back."""

    inbox_id: str = field(metadata={"dex_json_name": "inboxId"})
    conversation_id: str = field(metadata={"dex_json_name": "conversationId"})
    status: str
    messages: tuple[ConversationMessage, ...]
    private_permalink: str = field(
        default="", metadata={"dex_json_name": "privatePermalink", "dex_json_omit_default": True}
    )


received_conversation_attribute = dex.Attribute("hiver-received-conversation", ReceivedConversation)
intake_outcome_attribute = dex.Attribute("hiver-conversation-intake", IntakeOutcome)


def accept_every_conversation(event: TriggerEvent[ConversationReceivedEvent]) -> bool:
    """Admit every new conversation; an application narrows it, for example to open ones."""
    return True


def resolve_flow_id(event: TriggerEvent[ConversationReceivedEvent]) -> str:
    """Name one Flow per conversation, so a redelivered conversation finds its existing Flow."""
    return FLOW_ID_PREFIX + event.id


def map_to_flow_input(event: TriggerEvent[ConversationReceivedEvent]) -> ReceivedConversation:
    """Keep the Trigger event ID beside the delivered conversation."""
    return ReceivedConversation(event.id, event.payload, event.occurred_at)


def map_to_get_conversation_input(input: ReceivedConversation) -> GetConversationInput:
    """Read the delivered conversation from its inbox."""
    return GetConversationInput(input.received.inbox_id, input.received.conversation.id)


def validate_received_conversation(input: ReceivedConversation) -> str:
    """Return why a delivery is unusable, or "" when it names its event, inbox, and conversation."""
    if not (
        input.trigger_event_id.strip()
        and input.received.inbox_id.strip()
        and input.received.conversation.id.strip()
    ):
        return "a received conversation requires its Trigger event ID, inbox ID, and conversation ID"
    return ""


# dex:group group-id:intake group-label:"Intake"
# dex:explanation text:"Record the status and message IDs as the outcome and complete the Flow."
class CompleteConversationIntake(dex.Step[QueryResult[ConversationDetails]]):
    """Record the read-back outcome and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteConversationIntake"

    def execute(self, context: dex.Context, input: QueryResult[ConversationDetails]) -> dex.StepDecision:
        """Keep the conversation's current status and message IDs."""
        details = input.value
        outcome = IntakeOutcome(
            details.conversation.inbox_id,
            details.conversation.id,
            details.conversation.status,
            tuple(details.messages),
            details.conversation.private_permalink,
        )
        intake_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:intake group-label:"Intake"
# dex:explanation text:"Record the delivered conversation, then read it from Hiver."
class RecordReceivedConversation(dex.Step[ReceivedConversation]):
    """Validate and record the delivered conversation."""

    def __init__(self, read_received_conversation: dex.StepTarget[ReceivedConversation]) -> None:
        """Bind the read-back Step this Step continues to."""
        self.read_received_conversation = read_received_conversation

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordReceivedConversation"

    def execute(self, context: dex.Context, input: ReceivedConversation) -> dex.StepDecision:
        """Fail an unusable delivery; record anything else and read it back."""
        problem = validate_received_conversation(input)
        if problem:
            return dex.force_fail(problem)
        received_conversation_attribute.set(context, input)
        return dex.go_to(self.read_received_conversation, input)


class NewConversationIntakeFlow(dex.Flow[ReceivedConversation]):
    """Record one new Hiver conversation and its message IDs."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Hiver connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"new conversation intake Flow requires the {CONNECTION_NAME} connection")
        self.complete = CompleteConversationIntake()
        self.read_received_conversation = new_get_conversation_step(
            step_type="ReadReceivedConversation",
            step_input_type=ReceivedConversation,
            annotations={
                "groupId": "intake",
                "groupLabel": "Intake",
                "explanation": "Read the new conversation from Hiver for its current status and message IDs.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_conversation_input,
            found=self.complete,
        )
        self.record = RecordReceivedConversation(self.read_received_conversation)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "HiverNewConversationIntakePython"

    def get_steps(self) -> dex.StepList[ReceivedConversation]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record).other_steps(
            self.read_received_conversation, self.complete
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the delivery and outcome Attributes."""
        return dex.PersistenceSchema.of(received_conversation_attribute, intake_outcome_attribute)

    # Dex Web configures this static binding with the inbox picker; the analyzer requires a lone return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_conversation_received_trigger_binding(
                connection_name="hiver-support",
                binding_name="new-support-conversations",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "sharedInbox",
                            "inboxPicker",
                            "Shared inbox",
                            "Choose the Hiver shared inbox whose new conversations start this Flow. Load "
                            "inboxes lists the account's shared inboxes with the connection's API key and "
                            "saves the chosen inbox's ID. To enter the ID instead, a Hiver admin finds it "
                            "as the id of the inbox in GET https://api2.hiverhq.com/v1/inboxes; it is a "
                            "short string of digits such as 105902 and is not secret. Conversations already"
                            " in the inbox when the Worker first polls never start Flows. Restart the "
                            "Worker after saving; choosing another inbox records a new baseline. It cannot "
                            "be blank.",
                            True,
                            (ConnectorUIBinding("inboxId", "/inboxId"),),
                        ),
                    )
                ),
            )
        ]
