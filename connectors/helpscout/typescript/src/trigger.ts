// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac, timingSafeEqual } from 'node:crypto';
import type { IncomingHttpHeaders, IncomingMessage, ServerResponse } from 'node:http';
import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectionRef, type ConnectorConfigurationUI, type CredentialRefreshDriver, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition,
  type TriggerEvent, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import {
  decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type Configuration as ProjectConfiguration, type LoadedProject, type TriggerInbox, type TriggerInboxKey,
} from '@superdurable/connectors-sdk/projectconfig';
import { WebhookEndpoint, WebhookEndpointRunner, WebhookVerificationUnavailableError, type WebhookDecodeResult, type WebhookRequest } from '@superdurable/connectors-sdk/webhooktrigger';
import { z } from 'zod';
import { HelpScoutClient, type HelpScoutClientOptions } from './client.js';
import { decodeConversation } from './decode.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { ConversationSchema, type Conversation, type Credentials } from './generated/models.js';
import { newProjectConnection } from './project.js';

/** Sent when a conversation is created. */ export const WebhookEventConversationCreated = 'convo.created';
/** Sent when a conversation's assignee changes. */ export const WebhookEventConversationAssigned = 'convo.assigned';
/** Sent when a conversation's status changes. */ export const WebhookEventConversationStatusUpdated = 'convo.status';
/** Sent when a conversation's tags change. */ export const WebhookEventConversationTagsUpdated = 'convo.tags';
/** Sent when a conversation's custom fields change. */ export const WebhookEventConversationCustomFieldsUpdated = 'convo.custom-fields';
/** Sent when a conversation moves to another inbox. */ export const WebhookEventConversationMoved = 'convo.moved';
/** Sent when a conversation is merged into another. */ export const WebhookEventConversationMerged = 'convo.merged';
/** Sent when a customer replies. */ export const WebhookEventCustomerReplyCreated = 'convo.customer.reply.created';
/** Sent when a user replies; Help Scout omits auto replies. */ export const WebhookEventAgentReplyCreated = 'convo.agent.reply.created';
/** Sent when a user adds an internal note. */ export const WebhookEventNoteCreated = 'convo.note.created';

/** Help Scout inbox picker UI unit, which stores a stable inbox ID. */ export const UIUnitMailboxPicker = 'mailboxPicker';
/** Inbox picker output port. */ export const UIMailboxPickerPortMailboxID = 'mailboxId';

/** Carries the base64 HMAC-SHA1 of the raw body keyed with the webhook secret. */
const helpScoutSignatureHeader = 'x-helpscout-signature';
/** Names the webhook event, such as convo.created. */
const helpScoutEventHeader = 'x-helpscout-event';
const webhookEventNamePattern = /^[a-z][a-z0-9.-]{0,63}$/;
/** Standard padded base64, as Go's base64.StdEncoding accepts it after removing line breaks. */
const standardBase64Pattern = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;
const sha1Bytes = 20;

/**
 * The Help Scout webhook events whose body is a conversation, which the conversationEvent Trigger
 * decodes. Help Scout documents each with a v2 Conversation object body.
 */
export function conversationWebhookEvents(): string[] {
  return [
    WebhookEventConversationCreated, WebhookEventConversationAssigned, WebhookEventConversationStatusUpdated,
    WebhookEventConversationTagsUpdated, WebhookEventConversationCustomFieldsUpdated, WebhookEventConversationMoved,
    WebhookEventConversationMerged, WebhookEventCustomerReplyCreated, WebhookEventAgentReplyCreated, WebhookEventNoteCreated,
  ];
}

/**
 * Filters the Help Scout webhook events one binding records. The empty configuration records every
 * conversation event of every inbox. It reduces what a binding stores; the application's TriggerFilter
 * remains its admission rule.
 */
export const ConversationEventTriggerConfigurationSchema = z.strictObject({
  /** Conversation webhook events from conversationWebhookEvents; empty accepts every one. */ events: z.array(z.string()).nullable().optional(),
  /** Accept only conversations of this inbox, such as the mailboxPicker unit's mailboxId; zero or null, as Go reads it, accepts every inbox. */ mailboxId: z.number().int().nullable().optional(),
});
/** Event and inbox filter of one conversationEvent binding. */
export type ConversationEventTriggerConfiguration = z.infer<typeof ConversationEventTriggerConfigurationSchema>;

/** Check that events holds distinct conversation webhook events and mailboxId is not negative. */
export function validateConversationEventTriggerConfiguration(configuration: ConversationEventTriggerConfiguration): void {
  const seen = new Set<string>();
  for (const event of configuration.events ?? []) {
    if (!conversationWebhookEvents().includes(event)) throw new TypeError(`conversationEvent event ${JSON.stringify(event)} is not a Help Scout conversation webhook event`);
    if (seen.has(event)) throw new TypeError(`conversationEvent events lists ${JSON.stringify(event)} twice`);
    seen.add(event);
  }
  if ((configuration.mailboxId ?? 0) < 0) throw new TypeError('conversationEvent mailboxId must be a positive Help Scout inbox ID, or zero for every inbox');
}

/**
 * One verified Help Scout conversation webhook. The Trigger event ID is the event, the conversation ID,
 * and the first 32 hex digits of the body's SHA-256, such as convo.created:123456:0f1e..., because Help
 * Scout sends no delivery ID: a redelivery of the same body keeps its ID, and a later event about the
 * same conversation has another. The event's occurredAt is when this process received the request,
 * because Help Scout sends no event time. Field names and order match the Go connector.
 */
export const ConversationEventSchema = z.strictObject({
  /** The X-HelpScout-Event value, such as convo.customer.reply.created. */ event: z.string(),
  /** The conversation as the webhook body described it; its threads are not included, so a Flow reads them with getConversation. */ conversation: ConversationSchema,
});
/** One verified Help Scout conversation webhook. */
export interface ConversationEvent { event: string; conversation: Conversation }

/** Manifest definition of conversationEvent. */
export const ConversationEventTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'conversationEvent' },
  description: 'Receive a signed Help Scout conversation webhook, such as convo.created or convo.customer.reply.created, filtered by event and inbox.',
};

/** One static application binding of the conversationEvent Trigger. */
export interface ConversationEventTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the mailboxPicker unit. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static conversationEvent binding for Flow Definition tooling and Dex Web. */
export function defineConversationEventTriggerBinding(config: ConversationEventTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ConversationEventTriggerDefinition, ...config });
}

/** Configuration of one conversationEvent Trigger. */
export interface ConversationEventTriggerConfig {
  /** Help Scout connection bound to a HelpScoutClient, from newProjectConnection or newConnection. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Event and inbox filter of the binding; omitted records every conversation event of every inbox. */ configuration?: ConversationEventTriggerConfiguration;
  /** Routes accepted events into the application, such as newDexFlowTriggerTarget. */ target: TriggerTarget<ConversationEvent>;
}

/**
 * Create one conversationEvent Trigger fed by the connection's shared webhook endpoint, which an
 * application mounts with conversationEventWebhookHandler. It receives events only while running.
 * Throws for an invalid connection, a connection name that differs from the connection's, or an
 * invalid configuration.
 */
export function newConversationEventTrigger(config: ConversationEventTriggerConfig): TriggerRunner {
  const client = helpScoutClientOf(config.connection);
  if (config.connectionName !== config.connection.reference.name) {
    throw new TypeError(`helpscout connector trigger connectionName ${JSON.stringify(config.connectionName)} must equal its connection's name ${JSON.stringify(config.connection.reference.name)}`);
  }
  const configuration = ConversationEventTriggerConfigurationSchema.parse(config.configuration ?? {});
  validateConversationEventTriggerConfiguration(configuration);
  const endpoint = conversationEventWebhookEndpoint(client, config.connection.reference);
  const logger = client.webhookSettings().logger;
  return newTrigger({
    definition: ConversationEventTriggerDefinition,
    binding: { connection: config.connection.reference, trigger: ConversationEventTriggerDefinition.trigger, name: config.bindingName },
    source: endpoint.newSource(event => acceptsConversationEvent(configuration, event)),
    target: config.target,
    ...(logger ? { logger } : {}),
  });
}

/**
 * Return the connection's webhook endpoint for an application to mount at the public HTTPS URL of its
 * Help Scout webhook; its serveHTTP is a node:http request listener. Every conversationEvent Trigger
 * built from this connection feeds from it, and it answers 503 while none of them runs, so Help Scout
 * retries.
 */
export function conversationEventWebhookHandler(connection: Connection): WebhookEndpoint<Credentials, ConversationEvent> {
  return conversationEventWebhookEndpoint(helpScoutClientOf(connection), connection.reference);
}

/** One conversationEvent binding from the project configuration and its application target. */
export interface ProjectConversationEventTriggerRoute {
  /** Configured binding name whose configuration filters the events. */ bindingName: string;
  /** Receives the binding's events through its durable project inbox, such as newDexFlowTriggerTarget. */ target: TriggerTarget<ConversationEvent>;
}

/**
 * Serves one connection's Help Scout webhook endpoint while running its bindings. Mount serveHTTP at the
 * webhook's URL and run it for the life of the process. It is safe for concurrent requests.
 */
export class ConversationEventEndpointRunner {
  /** @internal Use newProjectConversationEventEndpointRunner. */
  constructor(private readonly endpointRunner: WebhookEndpointRunner, private readonly endpoint: WebhookEndpoint<Credentials, ConversationEvent>) {}

  /** Verify, record, and acknowledge one Help Scout delivery; answers 503 until run has replayed every binding's pending events. */
  serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> { return this.endpointRunner.serveHTTP(request, response); }

  /**
   * Replay each binding's durable inbox and then deliver new events until the context ends or a binding
   * fails. Rejects with TriggerDeliveryAbortedError after the context ends, otherwise with the failure.
   */
  run(context: TriggerContext): Promise<void> { return this.endpointRunner.run(context); }

  /** How many bindings are receiving events; until it is positive, every delivery is answered 503. */
  runningSourceCount(): number { return this.endpoint.runningSourceCount(); }
}

/**
 * Open connectionName with newProjectConnection and every route's binding from the loaded project
 * configuration, and wrap each target in the binding's durable project inbox, so an event is stored
 * before Help Scout receives 200. Throws when routes are empty, a binding name is blank or duplicated, or
 * a route's binding is not configured or invalid. options.logger also applies to the inboxes.
 */
export function newProjectConversationEventEndpointRunner(
  project: LoadedProject, connectionName: string, routes: readonly ProjectConversationEventTriggerRoute[], options: HelpScoutClientOptions = {},
): ConversationEventEndpointRunner {
  const connection = newProjectConnection(project, connectionName, options);
  return newConversationEventEndpointRunnerWithInboxes(connection, project.configuration, routes, key => project.triggerInbox(key));
}

/** @internal Run routes on connection with the bindings configuration stores, keeping each binding's events in openInbox(key). */
export function newConversationEventEndpointRunnerWithInboxes(
  connection: Connection, configuration: ProjectConfiguration, routes: readonly ProjectConversationEventTriggerRoute[], openInbox: (key: TriggerInboxKey) => TriggerInbox,
): ConversationEventEndpointRunner {
  if (routes.length === 0) throw new TypeError('Help Scout conversationEvent routes are required');
  const client = helpScoutClientOf(connection);
  const endpoint = conversationEventWebhookEndpoint(client, connection.reference);
  const connectionName = connection.reference.name;
  const triggerName = ConversationEventTriggerDefinition.trigger.triggerName;
  const logger = client.webhookSettings().logger;
  const bindingNames = new Set<string>();
  const runners = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('Help Scout conversationEvent binding name and target are required');
    if (bindingNames.has(bindingName)) throw new TypeError(`Help Scout conversationEvent binding name ${JSON.stringify(bindingName)} is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    let bindingConfiguration: ConversationEventTriggerConfiguration;
    try { bindingConfiguration = decodeTriggerConfiguration(configuration, key, ConversationEventTriggerConfigurationSchema); }
    catch (error) { throw new Error(`Help Scout conversationEvent binding ${JSON.stringify(bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    try { validateConversationEventTriggerConfiguration(bindingConfiguration); }
    catch (error) { throw new Error(`binding ${JSON.stringify(bindingName)}: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    const target = newDurableTriggerTarget(openInbox(key), key, route.target, {
      ...(logger ? { logger } : {}), decodePayload: (_payloadJSON, payloadBytes) => ConversationEventSchema.parse(parseTriggerPayloadJSON(payloadBytes)),
    });
    return newConversationEventTrigger({ connection, connectionName, bindingName, configuration: bindingConfiguration, target });
  });
  return new ConversationEventEndpointRunner(new WebhookEndpointRunner(endpoint, runners), endpoint);
}

/** Each client's shared endpoint per connection, so every binding of a connection feeds from one URL. */
const conversationEventEndpoints = new WeakMap<HelpScoutClient, Map<string, WebhookEndpoint<Credentials, ConversationEvent>>>();

function conversationEventWebhookEndpoint(client: HelpScoutClient, connection: ConnectionRef): WebhookEndpoint<Credentials, ConversationEvent> {
  let endpoints = conversationEventEndpoints.get(client);
  if (!endpoints) { endpoints = new Map(); conversationEventEndpoints.set(client, endpoints); }
  const cacheKey = `${connection.provider}\u0000${connection.name}`;
  const cached = endpoints.get(cacheKey);
  if (cached) return cached;
  const settings = client.webhookSettings();
  const endpoint = new WebhookEndpoint<Credentials, ConversationEvent>({
    connectorId: ConnectorID, triggerName: ConversationEventTriggerDefinition.trigger.triggerName, connection,
    credentials: settings.credentials, credentialRefresh: newExpiredRecordRefreshDriver(settings.refreshDriver),
    maxBodyBytes: settings.maxBodyBytes, verifyRequest: verifyConversationWebhookRequest, decodeEvent: decodeConversationWebhookRequest,
    now: settings.now, ...(settings.logger ? { logger: settings.logger } : {}),
  });
  endpoints.set(cacheKey, endpoint);
  return endpoint;
}

/** Renew only a record project storage refuses as expired; verification needs only the webhook secret. */
function newExpiredRecordRefreshDriver(tokenDriver: CredentialRefreshDriver<Credentials>): CredentialRefreshDriver<Credentials> {
  return {
    refreshRequired: state => state.expiresAt !== undefined && state.now.getTime() >= state.expiresAt.getTime(),
    refresh: (state, signal) => tokenDriver.refresh(state, signal),
  };
}

/** Apply the binding's event and inbox filters. */
function acceptsConversationEvent(configuration: ConversationEventTriggerConfiguration, event: TriggerEvent<ConversationEvent>): boolean {
  const events = configuration.events ?? [];
  if (events.length > 0 && !events.includes(event.payload.event)) return false;
  const mailboxId = configuration.mailboxId ?? 0;
  return mailboxId === 0 || mailboxId === event.payload.conversation.mailboxId;
}

/** Answer 503 without a webhook secret, so Help Scout keeps retrying. */
async function verifyConversationWebhookRequest(request: WebhookRequest, credentials: Credentials): Promise<void> {
  const secret = credentials.webhookSecret?.reveal() ?? '';
  if (!secret) throw new WebhookVerificationUnavailableError();
  verifyHelpScoutSignature(request.body, firstHeaderValue(request.headers, helpScoutSignatureHeader), secret);
}

/** Compare base64(HMAC-SHA1(body, secret)) in constant time; Help Scout signs no timestamp. */
export function verifyHelpScoutSignature(body: Uint8Array, header: string, secret: string): void {
  const encoded = header.trim().replace(/[\r\n]/g, '');
  const signature = standardBase64Pattern.test(encoded) ? Buffer.from(encoded, 'base64') : undefined;
  if (!signature || signature.length !== sha1Bytes) throw new HelpScoutSignatureInvalidError();
  const expected = createHmac('sha1', secret).update(body).digest();
  if (!timingSafeEqual(signature, expected)) throw new HelpScoutSignatureInvalidError();
}

/** X-HelpScout-Signature is missing, malformed, or does not match. */
export class HelpScoutSignatureInvalidError extends Error {
  /** Create the fixed, secret-free error. */
  constructor() { super('X-HelpScout-Signature is missing, malformed, or does not match'); this.name = 'HelpScoutSignatureInvalidError'; }
}

/** Acknowledge, without a record, events outside conversationWebhookEvents. */
function decodeConversationWebhookRequest(request: WebhookRequest): WebhookDecodeResult<ConversationEvent> {
  const event = firstHeaderValue(request.headers, helpScoutEventHeader).trim();
  if (!webhookEventNamePattern.test(event)) throw new TypeError('X-HelpScout-Event is missing or malformed');
  if (!conversationWebhookEvents().includes(event)) return undefined;
  if (request.body.length === 0) throw new TypeError('Help Scout returned a malformed response');
  let wire: unknown;
  try { wire = JSON.parse(Buffer.from(request.body).toString('utf8')); }
  catch { throw new TypeError('Help Scout returned a malformed response'); }
  const conversation = decodeConversation(wire);
  const bodyDigest = createHash('sha256').update(request.body).digest().subarray(0, 16).toString('hex');
  return { id: `${event}:${conversation.id}:${bodyDigest}`, occurredAt: request.receivedAt, payload: { event, conversation } };
}

/** The header's value; node:http joins a repeated header with ", ", so it answers 400 unlike Go. */
function firstHeaderValue(headers: IncomingHttpHeaders, name: string): string {
  const value = headers[name];
  return (Array.isArray(value) ? value[0] : value) ?? '';
}

function helpScoutClientOf(connection: Connection): HelpScoutClient {
  if (!(connection?.operations instanceof HelpScoutClient)) throw new TypeError('Help Scout Triggers require a connection bound to a HelpScoutClient');
  return connection.operations;
}
