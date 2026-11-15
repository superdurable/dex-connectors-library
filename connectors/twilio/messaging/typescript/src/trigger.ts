// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { TwilioClient, isE164PhoneNumber, isWhatsApp } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { MessageSchema } from './generated/models.js';
import { InboundMessageFetcher, inboundSnapshotOptions } from './inbound_message_fetcher.js';

/** Twilio's direction value for a message the account received. */
export const InboundMessageDirection = 'inbound';
/** Twilio received an inbound message and is still processing it, such as fetching MMS media. */
export const MessageStatusReceiving = 'receiving';
/** Twilio finished processing an inbound message. */
export const MessageStatusReceived = 'received';
/** Receiving address UI unit, which saves to. */ export const UIUnitReceivingAddressInput = 'receivingAddressInput';
/** Receiving address output port. */ export const UIReceivingAddressInputPortTo = 'to';

/**
 * The messageReceived binding configuration the receivingAddressInput unit saves in Dex Web. to is the
 * receiving Twilio number in E.164 form, such as +14155550100, or a WhatsApp sender written
 * whatsapp:+14155550100; blank receives every inbound message of the connection's account. Changing it for a
 * binding that already polled makes its next poll record a new baseline.
 */
export const MessageReceivedTriggerConfigurationSchema = z.strictObject({
  /** The receiving number or WhatsApp sender; blank receives every inbound message of the account. */
  to: z.string()
    .refine(value => value === value.trim(), 'messageReceived to must not have surrounding whitespace')
    .refine(value => value === '' || isE164PhoneNumber(value) || isWhatsApp(value), 'messageReceived to must be an E.164 phone number or a whatsapp:+E.164 address')
    .optional(),
});
/** The receiving address one messageReceived binding watches. */
export type MessageReceivedTriggerConfiguration = z.infer<typeof MessageReceivedTriggerConfigurationSchema>;

/**
 * One inbound message read from the Twilio Messages list; field names match the Go connector. Unlike
 * operation results, it carries the message text, because acting on an inbound message needs it. Media URLs,
 * Twilio error text, and credentials are never included. The Trigger event ID is "twilio-message-" followed by
 * the message SID.
 */
export const MessageReceivedEventSchema = z.strictObject({
  /** The safe message view; status is received, or receiving when processing outlasted the hold limit. */ message: MessageSchema,
  /** The inbound text, omitted for a media-only MMS; Twilio may redact it per the account's retention settings. */ body: z.string().optional(),
});
/** One inbound message. */
export type MessageReceivedEvent = z.infer<typeof MessageReceivedEventSchema>;

/** Manifest definition of messageReceived. */
export const MessageReceivedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'messageReceived' },
  description: 'Receive each inbound SMS, MMS, or WhatsApp message to the account, or to one Twilio number, read by polling the Twilio Messages list; the first run records a baseline and delivers no existing messages.',
};

/** One static application binding of messageReceived. */
export interface MessageReceivedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the receivingAddressInput unit. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static messageReceived binding for Flow Definition tooling and Dex Web. */
export function defineMessageReceivedTriggerBinding(config: MessageReceivedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: MessageReceivedTriggerDefinition, ...config });
}

/** Decode a replayed message, including one a Go replica recorded. */
export function decodeMessageReceivedPayload(_payloadJSON: string, payloadBytes: Uint8Array): MessageReceivedEvent {
  const decoded = MessageReceivedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes));
  if (!decoded.message.sid) throw new TypeError('stored Twilio inbound message is invalid');
  return decoded;
}

function twilioClientOf(connection: Connection): TwilioClient {
  if (!(connection?.operations instanceof TwilioClient)) throw new TypeError('Twilio Messaging Triggers require a connection from newConnection or newProjectConnection, not a mock connection');
  return connection.operations;
}

function pollScheduleOf(client: TwilioClient): PollSchedule {
  // An omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
  return { intervalMs: client.pollIntervalMs };
}

function parseMessageReceivedTriggerConfiguration(configuration: MessageReceivedTriggerConfiguration): MessageReceivedTriggerConfiguration {
  const parsed = MessageReceivedTriggerConfigurationSchema.safeParse(configuration ?? {});
  if (!parsed.success) throw new TypeError(`twilio-messaging messageReceived configuration: ${parsed.error.issues[0]?.message ?? 'invalid'}`);
  return parsed.data;
}

/** Binds one messageReceived configuration to its application target and cursor. */
export interface MessageReceivedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the receiving address; blank receives every inbound message of the account. */ configuration: MessageReceivedTriggerConfiguration;
  /**
   * Routes each inbound message into the application. A target from newDurableTriggerTarget records each
   * message before the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<MessageReceivedEvent>;
  /**
   * Stores the binding's DateSent watermark and delivered-message snapshot, such as LoadedProject.pollCursorStore.
   * Omitted keeps it in memory, so every process start records a new baseline and messages received while
   * stopped are lost.
   */
  cursor?: CursorStore | undefined;
}

/**
 * Create one poll runner for the messageReceived bindings polled through one connection. run replays each
 * durable target, records a baseline for a binding without a cursor, then lists each binding's Messages each
 * pollInterval with jitter. A cursor for another account or receiving address records a new baseline and logs
 * a WARN record; messages received between the last poll and the new baseline are not delivered. A 429 pauses
 * every binding of the connection for Retry-After, or backs off exponentially without it; other failures back
 * off per binding up to 15 minutes. Delivery is at least once, and each message keeps one Trigger event ID,
 * from its SID, across refetches. logger receives records with binding identity, never message bodies,
 * addresses, or credentials. Throws for no routes, a blank or repeated binding name, a missing target, an
 * invalid configuration, or a mock connection.
 */
export function newMessageTriggerRunner(connection: Connection, routes: MessageReceivedTriggerRoute[], logger?: TriggerLogger): Runner {
  const client = twilioClientOf(connection);
  if (!routes?.length) throw new TypeError('Twilio messageReceived routes are required');
  const pollers: Poller[] = routes.map(route => {
    if (!route.bindingName?.trim() || !route.target) throw new TypeError('Twilio messageReceived binding name and target are required');
    const configuration = parseMessageReceivedTriggerConfiguration(route.configuration);
    return newBindingPoller({
      definition: MessageReceivedTriggerDefinition,
      binding: { connection: connection.reference, trigger: MessageReceivedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(), fetcher: new InboundMessageFetcher(client, connection.reference, configuration.to ?? ''),
      target: route.target, snapshot: inboundSnapshotOptions,
    });
  });
  return newRunner({ connection: connection.reference, schedule: pollScheduleOf(client), pollers, ...(logger ? { logger } : {}) });
}

/** Selects one messageReceived binding saved in the project configuration. */
export interface ProjectMessageReceivedTriggerRoute {
  /** The saved binding's name. */ bindingName: string;
  /** Routes each inbound message into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<MessageReceivedEvent>;
}

/**
 * Create a durable runner for messageReceived bindings saved in the loaded project. connection is the
 * project's connection from newProjectConnection; the caller shares it with the Flow. Each binding's saved
 * receiving address is loaded (an unsaved binding receives every inbound message of the account), and each
 * route records messages in its binding's project Trigger inbox before its watermark commits in the binding's
 * project poll cursor, so a restarted or replaced replica neither misses nor repeats a message. logger also
 * receives the inboxes' records. Throws for an invalid saved configuration or route, as newMessageTriggerRunner does.
 */
export function newProjectMessageTriggerRunner(project: LoadedProject, connection: Connection, routes: ProjectMessageReceivedTriggerRoute[], logger?: TriggerLogger): Runner {
  if (!project) throw new TypeError('Twilio Trigger runner requires the loaded project configuration');
  twilioClientOf(connection);
  const triggerName = MessageReceivedTriggerDefinition.trigger.triggerName;
  return newMessageTriggerRunner(connection, (routes ?? []).map(route => {
    if (!route.target) throw new TypeError(`Twilio messageReceived binding "${route.bindingName}" target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName, bindingName: route.bindingName };
    let configuration: MessageReceivedTriggerConfiguration = {};
    try { configuration = decodeTriggerConfiguration(project.configuration, key, MessageReceivedTriggerConfigurationSchema); }
    catch (error) {
      // An unsaved binding receives every inbound message, as a blank address does.
      if (!(error instanceof ObjectNotFoundError)) throw new TypeError(`twilio-messaging messageReceived binding "${route.bindingName}" configuration: ${error instanceof Error ? error.message : 'invalid'}`);
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeMessageReceivedPayload, ...(logger ? { logger } : {}) });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  }), logger);
}

/** Configuration of one messageReceived Trigger with an in-memory cursor. */
export interface MessageReceivedTriggerConfig {
  /** Twilio connection from newProjectConnection or newConnection. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** The receiving address; blank receives every inbound message. */ configuration: MessageReceivedTriggerConfiguration;
  /** Routes accepted events into the application. */ target: TriggerTarget<MessageReceivedEvent>;
  /** Receives the runner's records; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger | undefined;
}

/**
 * Create one messageReceived binding's TriggerRunner, as Go's generated NewMessageReceivedTrigger does. It has
 * no project storage, so it keeps the cursor in memory and records a new baseline on every start; use
 * newProjectMessageTriggerRunner for a durable cursor and inbox. Throws for a mismatched connection name, an
 * invalid configuration, or a mock connection.
 */
export function newMessageReceivedTrigger(config: MessageReceivedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError(`twilio-messaging connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  const client = twilioClientOf(config.connection);
  const configuration = parseMessageReceivedTriggerConfiguration(config.configuration);
  const binding = { connection: config.connection.reference, trigger: MessageReceivedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource({
    definition: MessageReceivedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new InboundMessageFetcher(client, config.connection.reference, configuration.to ?? ''), snapshot: inboundSnapshotOptions,
  }, pollScheduleOf(client), config.logger);
  return newTrigger({ definition: MessageReceivedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}
