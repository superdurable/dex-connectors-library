# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Flow per inbound Twilio message, read it back, and record its current status."""

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

from dex_connectors.twilio_messaging import (
    INBOUND_MESSAGE_DIRECTION,
    Connection,
    GetMessageInput,
    Message,
    MessageReceivedEvent,
    define_message_received_trigger_binding,
    new_get_message_step,
)

FLOW_TYPE = "TwilioInboundMessagePython"
CONNECTION_NAME = "twilio-messaging"
MESSAGE_RECEIVED_TRIGGER_BINDING = "inbound-messages"
READ_INBOUND_MESSAGE_STEP_TYPE = "ReadInboundMessage"

# Twilio's default opt-out keywords, matched on the whole trimmed text.
OPT_OUT_KEYWORDS = frozenset({"STOP", "STOPALL", "UNSUBSCRIBE", "CANCEL", "END", "QUIT", "REVOKE", "OPTOUT"})


@dataclass(frozen=True, slots=True)
class InboundMessageInput:
    """One messageReceived delivery with the Trigger event ID that also names the Flow."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    received: MessageReceivedEvent = field(metadata={"dex_json_name": "received"})


@dataclass(frozen=True, slots=True)
class InboundMessageOutcome:
    """The Flow result: the message, whether it opts out, and the message read back from Twilio."""

    message_sid: str = field(metadata={"dex_json_name": "messageSid"})
    from_: str = field(metadata={"dex_json_name": "from"})
    to: str = field(metadata={"dex_json_name": "to"})
    is_opt_out: bool = field(metadata={"dex_json_name": "isOptOut"})
    current: Message = field(metadata={"dex_json_name": "current"})


inbound_message_attribute = dex.Attribute("twilio-inbound-message", InboundMessageInput)
inbound_message_outcome_attribute = dex.Attribute("twilio-inbound-message-outcome", InboundMessageOutcome)


def resolve_flow_id(event: TriggerEvent[MessageReceivedEvent]) -> str:
    """Name one Flow per inbound message; a redelivered message finds its existing Flow."""
    return "twilio-inbound-message-" + event.id


def map_to_flow_input(event: TriggerEvent[MessageReceivedEvent]) -> InboundMessageInput:
    """Keep the Trigger event ID beside the delivered message."""
    return InboundMessageInput(event.id, event.payload)


def accept_every_inbound_message(event: TriggerEvent[MessageReceivedEvent]) -> bool:
    """Admit every message; an application narrows it, for example by sender."""
    return True


def map_to_get_message_input(input: InboundMessageInput) -> GetMessageInput:
    """Read the inbound message by its SID."""
    return GetMessageInput(input.received.message.sid)


def is_opt_out_text(body: str) -> bool:
    """Report whether body is one of Twilio's default opt-out keywords."""
    return body.strip().upper() in OPT_OUT_KEYWORDS


def _invalid_input_reason(input: InboundMessageInput) -> str:
    message = input.received.message
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not message.sid.strip() or not message.to.strip():
        return "received.message.sid and received.message.to are required"
    if message.direction != INBOUND_MESSAGE_DIRECTION:
        return "received.message.direction must be inbound"
    return ""


# dex:group group-id:inbound group-label:"Inbound message"
# dex:explanation text:"Record the delivered inbound message, then read it back from Twilio."
class RecordInboundMessage(dex.Step[InboundMessageInput]):
    """Validate and persist the delivered message, then read it back."""

    def __init__(self, read_inbound_message: dex.StepTarget[InboundMessageInput]) -> None:
        """Bind the connector Step that reads the message back."""
        self._read_inbound_message = read_inbound_message

    def get_step_type(self) -> str:
        """Keep the Step identity stable."""
        return "RecordInboundMessage"

    def execute(self, context: dex.Context, input: InboundMessageInput) -> dex.StepDecision:
        """Fail an incomplete delivery; otherwise record it and read the message back."""
        reason = _invalid_input_reason(input)
        if reason:
            return dex.force_fail(reason)
        inbound_message_attribute.set(context, input)
        return dex.go_to(self._read_inbound_message, input)


# dex:group group-id:inbound group-label:"Inbound message"
# dex:explanation text:"Record the message read back from Twilio as the outcome and complete the Flow."
class CompleteInboundMessage(dex.Step[QueryResult[Message]]):
    """Record the current message and its opt-out flag as the outcome."""

    def get_step_type(self) -> str:
        """Keep the Step identity stable."""
        return "CompleteInboundMessage"

    def execute(self, context: dex.Context, input: QueryResult[Message]) -> dex.StepDecision:
        """Complete the Flow with the delivered message and the message read back."""
        received = inbound_message_attribute.get(context).received
        outcome = InboundMessageOutcome(
            received.message.sid,
            received.message.from_,
            received.message.to,
            is_opt_out_text(received.body),
            input.value,
        )
        inbound_message_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class InboundMessageFlow(dex.Flow[InboundMessageInput]):
    """Record one inbound Twilio message started by the messageReceived Trigger."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Twilio Messaging connection before Worker registration."""
        self.complete = CompleteInboundMessage()
        self.read_inbound_message = new_get_message_step(
            step_type=READ_INBOUND_MESSAGE_STEP_TYPE,
            step_input_type=InboundMessageInput,
            annotations={
                "groupId": "inbound",
                "groupLabel": "Inbound message",
                "explanation": "Read the inbound message back from Twilio, so the record holds its current "
                "status.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_message_input,
            found=self.complete,
        )
        self.record = RecordInboundMessage(self.read_inbound_message)

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "TwilioInboundMessagePython"

    def get_steps(self) -> dex.StepList[InboundMessageInput]:
        """Record the message, read it back, and complete."""
        return dex.StepList.start_step(self.record).other_steps(self.read_inbound_message, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the delivery and outcome Attributes."""
        return dex.PersistenceSchema.of(inbound_message_attribute, inbound_message_outcome_attribute)

    # Dex Web configures this static binding; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_message_received_trigger_binding(
                connection_name="twilio-messaging",
                binding_name="inbound-messages",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="receivingNumber",
                            unit_id="receivingAddressInput",
                            label="Receiving number",
                            description="Enter the Twilio number, such as +14155550100, or WhatsApp sender, such as whatsapp:+14155550100, whose inbound messages start this Flow; copy it from https://console.twilio.com/us1/develop/phone-numbers/manage/incoming. Leave it blank to start a Flow for every inbound message of the account. The first start records existing messages as a baseline and starts no Flow for them. Restart the Worker after saving; a changed number records a new baseline.",  # noqa: E501
                            required=False,
                            bindings=(ConnectorUIBinding(port="to", json_pointer="/to"),),
                        ),
                    )
                ),
            )
        ]
