// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  InboundMessageDirection, defineMessageReceivedTriggerBinding, newGetMessageStep,
  type Connection, type GetMessageInput, type Message, type MessageReceivedEvent,
} from '@superdurable/connector-twilio-messaging';

/** Stable Flow identity each inbound message starts. */
export const FlowType = 'TwilioInboundMessageTypeScript';
/** Static Dex Web connection for Twilio Messaging. */
export const ConnectionName = 'twilio-messaging';
/** The messageReceived binding that starts this Flow. */
export const MessageReceivedTriggerBinding = 'inbound-messages';
const readInboundMessageStepType = 'ReadInboundMessage';
/** Twilio's default opt-out keywords, matched on the whole trimmed text. */
const optOutKeywords = new Set(['STOP', 'STOPALL', 'UNSUBSCRIBE', 'CANCEL', 'END', 'QUIT', 'REVOKE', 'OPTOUT']);

/** One messageReceived delivery with the Trigger event ID that also names the Flow. */
export interface InboundMessageInput { /** Stable Trigger event ID. */ triggerEventId: string; /** The delivered message. */ received: MessageReceivedEvent }

/** The Flow result: the message, whether it opts out, and the message read back from Twilio. */
export interface InboundMessageOutcome {
  /** The inbound message. */ messageSid: string;
  /** The sender who texted the Twilio number. */ from: string;
  /** The Twilio number or WhatsApp sender that received it. */ to: string;
  /** Whether the text is a STOP-family keyword, which Twilio's default opt-out handling already honors. */ isOptOut: boolean;
  /** The message read back from Twilio with getMessage. */ current: Message;
}

/** The delivered message. */
export const inboundMessageAttribute = new Attribute<InboundMessageInput>('twilio-inbound-message', jsonCodec<InboundMessageInput>());
/** The recorded outcome. */
export const inboundMessageOutcomeAttribute = new Attribute<InboundMessageOutcome>('twilio-inbound-message-outcome', jsonCodec<InboundMessageOutcome>());

/** Name one Flow per inbound message; a redelivered message finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<MessageReceivedEvent>): string { return `twilio-inbound-message-${event.id}`; }
/** Keep the Trigger event ID beside the delivered message. */
export function mapToFlowInput(event: TriggerEvent<MessageReceivedEvent>): InboundMessageInput { return { triggerEventId: event.id, received: event.payload }; }
/** Admit every message; an application narrows it, for example by sender. */
export function acceptEveryInboundMessage(): boolean { return true; }
/** Read the inbound message by its SID. */
export function mapToGetMessageInput(input: InboundMessageInput): GetMessageInput { return { messageSid: input.received.message.sid ?? '' }; }
/** Whether body is one of Twilio's default opt-out keywords. */
export function isOptOutText(body: string): boolean { return optOutKeywords.has(body.trim().toUpperCase()); }

/** Return why a delivered message cannot be recorded, or an empty string. */
export function invalidInputReason(input: InboundMessageInput): string {
  const message = input.received?.message;
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!message?.sid?.trim() || !message.to?.trim()) return 'received.message.sid and received.message.to are required';
  if (message.direction !== InboundMessageDirection) return 'received.message.direction must be inbound';
  return '';
}

/** Records one inbound Twilio message started by the messageReceived Trigger. */
export class InboundMessageFlow implements Flow<InboundMessageInput> {
  private readonly record = new RecordInboundMessage();
  private readonly complete = new CompleteInboundMessage();
  /** Bind the Twilio Messaging connection before Worker registration. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'TwilioInboundMessageTypeScript'; }
  /** Record the message, read it back, and complete. */
  getSteps(): StepList<InboundMessageInput> {
    return StepList.startStep(this.record).otherSteps(
      newGetMessageStep<InboundMessageInput>({
        stepType: readInboundMessageStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'inbound', groupLabel: 'Inbound message', explanation: 'Read the inbound message back from Twilio, so the record holds its current status.' },
        mapToOperationInput: mapToGetMessageInput,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the delivery and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [inboundMessageAttribute, inboundMessageOutcomeAttribute] }; }
  /** Declare the messageReceived binding that starts this Flow, with its receiving number. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineMessageReceivedTriggerBinding({ connectionName: ConnectionName, bindingName: MessageReceivedTriggerBinding, configurationUI: { units: [{
      id: 'receivingNumber', unitId: 'receivingAddressInput', label: 'Receiving number', required: false,
      description: 'Enter the Twilio number, such as +14155550100, or WhatsApp sender, such as whatsapp:+14155550100, whose inbound messages start this Flow; copy it from https://console.twilio.com/us1/develop/phone-numbers/manage/incoming. Leave it blank to start a Flow for every inbound message of the account. The first start records existing messages as a baseline and starts no Flow for them. Restart the Worker after saving; a changed number records a new baseline.',
      bindings: [{ port: 'to', jsonPointer: '/to' }],
    }] } })];
  }
}

/** Validate and persist the delivered message, then read it back. */
// dex:group group-id:inbound group-label:"Inbound message"
// dex:explanation text:"Record the delivered inbound message, then read it back from Twilio."
export class RecordInboundMessage implements Step<InboundMessageInput> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordInboundMessage'; }
  /** Fail an incomplete delivery; otherwise record it and read the message back. */
  execute(context: Context, input: InboundMessageInput): StepDecision {
    const reason = invalidInputReason(input);
    if (reason) return forceFail(reason);
    inboundMessageAttribute.set(context, input);
    return goTo(new StepRef('ReadInboundMessage'), input);
  }
}

/** Record the current message and its opt-out flag as the outcome. */
// dex:group group-id:inbound group-label:"Inbound message"
// dex:explanation text:"Record the message read back from Twilio as the outcome and complete the Flow."
export class CompleteInboundMessage implements Step<QueryResult<Message>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteInboundMessage'; }
  /** Complete the Flow with the delivered message and the message read back. */
  execute(context: Context, result: QueryResult<Message>): StepDecision {
    const { received } = inboundMessageAttribute.get(context);
    const outcome: InboundMessageOutcome = {
      messageSid: received.message.sid ?? '', from: received.message.from ?? '', to: received.message.to,
      isOptOut: isOptOutText(received.body ?? ''), current: result.value,
    };
    inboundMessageOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
