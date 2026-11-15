// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, timingSafeEqual } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectionRef, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition, type TriggerEvent, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { WebhookEndpoint, WebhookEndpointRunner, WebhookVerificationUnavailableError, type WebhookRequest } from '@superdurable/connectors-sdk/webhooktrigger';
import { z } from 'zod';
import { CalendlyClient, type CalendlyClientOptions } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { InviteeSchema, ScheduledEventSchema, type Credentials } from './generated/models.js';
import { decodeEvent, decodeInvitee, parseCalendlyJSON, parseObject, parseResourceURI, rfc3339, stringField } from './operations.js';
import { newProjectConnection } from './project.js';

/** The Calendly webhook event sent when an invitee books an event. */
export const WebhookEventInviteeCreated = 'invitee.created';
/** The Calendly webhook event sent when an invitee or host cancels. */
export const WebhookEventInviteeCanceled = 'invitee.canceled';

/** Calendly event type picker UI unit, which stores a stable event type URI. */ export const UIUnitEventTypePicker = 'eventTypePicker';
/** Event type picker output port. */ export const UIEventTypePickerPortEventTypeURI = 'eventTypeUri';

/** Carries t=<unix seconds>,v1=<hex HMAC-SHA256 of "t.body">. */
const signatureHeader = 'calendly-webhook-signature';
const inviteeURIPattern = /^https:\/\/api\.calendly\.com\/scheduled_events\/([A-Za-z0-9_-]{1,64})\/invitees\/([A-Za-z0-9_-]{1,64})$/;

/**
 * Filters the Calendly webhook events one binding records. The empty configuration records every
 * invitee.created and invitee.canceled event. It reduces what a binding stores; the application's
 * TriggerFilter remains its admission rule.
 */
export const InviteeEventReceivedTriggerConfigurationSchema = z.strictObject({
  /** invitee.created and invitee.canceled; empty accepts both. */ events: z.array(z.string()).nullable().optional(),
  /** Accept only invitees of this event type, such as the eventTypePicker unit's eventTypeUri; blank accepts every event type. */ eventTypeUri: z.string().optional(),
}).superRefine((configuration, context) => {
  const events = configuration.events ?? [];
  if (events.some(event => event !== WebhookEventInviteeCreated && event !== WebhookEventInviteeCanceled) || new Set(events).size !== events.length) {
    context.addIssue({ code: 'custom', message: 'inviteeEventReceived events must be distinct values from invitee.created and invitee.canceled' });
  }
  if (configuration.eventTypeUri) {
    try { parseResourceURI(configuration.eventTypeUri, 'event_types', 'event type'); }
    catch (error) { context.addIssue({ code: 'custom', message: error instanceof Error ? error.message : 'event type URI is invalid' }); }
  }
});
/** Event and event type filter of one inviteeEventReceived binding. */
export type InviteeEventReceivedTriggerConfiguration = z.infer<typeof InviteeEventReceivedTriggerConfigurationSchema>;

/**
 * One verified invitee.created or invitee.canceled webhook; field names match the Go connector. The
 * Trigger event ID is the webhook event followed by the scheduled event and invitee IDs from the invitee's
 * stable URI, such as invitee.created:GBGBDCAADAEDCRZ2:AAAAAAAAAAAAAAAA, so a redelivery keeps its ID.
 */
export const InviteeEventSchema = z.object({
  /** invitee.created or invitee.canceled. */ event: z.string(),
  /** When Calendly created the webhook event, in UTC. */ createdAt: z.string(),
  /** The invitee who booked or canceled; a reschedule cancels the old invitee, with rescheduled set, and creates a new one. */ invitee: InviteeSchema,
  /** The scheduled event as the webhook described it. */ scheduledEvent: ScheduledEventSchema,
});
/** One verified invitee.created or invitee.canceled webhook. */
export type InviteeEvent = z.infer<typeof InviteeEventSchema>;

/** Manifest definition of inviteeEventReceived. */
export const InviteeEventReceivedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'inviteeEventReceived' },
  description: "Receive a signed Calendly invitee.created or invitee.canceled webhook, with the invitee's stable URI as its event ID.",
};

/** One static application binding of inviteeEventReceived. */
export interface InviteeEventReceivedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the eventTypePicker unit. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static inviteeEventReceived binding for Flow Definition tooling and Dex Web. */
export function defineInviteeEventReceivedTriggerBinding(config: InviteeEventReceivedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: InviteeEventReceivedTriggerDefinition, ...config });
}

/** Configuration of one inviteeEventReceived Trigger. */
export interface InviteeEventReceivedTriggerConfig {
  /** Calendly connection from newProjectConnection or newConnection with a CalendlyClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Event and event type filter. */ configuration: InviteeEventReceivedTriggerConfiguration;
  /** Routes accepted events into the application. */ target: TriggerTarget<InviteeEvent>;
}

/**
 * Create one inviteeEventReceived Trigger fed by the connection's webhook endpoint, which
 * inviteeEventReceivedWebhookHandler returns. Every Trigger of one connection shares that endpoint.
 */
export function newInviteeEventReceivedTrigger(config: InviteeEventReceivedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError(`calendly connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  const client = calendlyClientOf(config.connection);
  const configuration = InviteeEventReceivedTriggerConfigurationSchema.parse(config.configuration);
  const source = webhookEndpoint(client, config.connection.reference).newSource(event => acceptsEvent(configuration, event));
  const definition = InviteeEventReceivedTriggerDefinition;
  return newTrigger({ definition, binding: { connection: config.connection.reference, trigger: definition.trigger, name: config.bindingName }, source, target: config.target, logger: client.triggerLogger });
}

/** The webhook endpoint of one connection: a node:http request listener and its running source count. */
export interface InviteeEventReceivedWebhookHandler {
  /** Verify, record, and acknowledge one Calendly delivery. */ serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void>;
  /** How many bindings receive events; while zero every delivery is answered 503. */ runningSourceCount(): number;
}

/**
 * Return the connection's webhook endpoint for an application to mount at the public HTTPS callback URL of
 * its Calendly webhook subscription. Every inviteeEventReceived Trigger built from this connection feeds
 * from it, and it answers 503 while none of them runs, so Calendly retries.
 */
export function inviteeEventReceivedWebhookHandler(connection: Connection): InviteeEventReceivedWebhookHandler {
  return webhookEndpoint(calendlyClientOf(connection), connection.reference);
}

/** One inviteeEventReceived binding from the project configuration and its application target. */
export interface ProjectInviteeEventReceivedTriggerRoute {
  /** Configured binding name whose configuration filters the events. */ bindingName: string;
  /** Receives the binding's events through its durable project inbox, such as a newDexFlowTriggerTarget that starts one Flow per event ID. */ target: TriggerTarget<InviteeEvent>;
}

/**
 * Serves one connection's Calendly webhook endpoint while running its bindings. An application mounts
 * serveHTTP at its subscription's callback URL and runs it for the life of the process.
 */
export class InviteeEventReceivedEndpointRunner {
  readonly #endpointRunner: WebhookEndpointRunner;
  readonly #endpoint: WebhookEndpoint<Credentials, InviteeEvent>;

  /** @internal Use newProjectInviteeEventReceivedEndpointRunner. */
  constructor(endpoint: WebhookEndpoint<Credentials, InviteeEvent>, runners: readonly TriggerRunner[]) {
    this.#endpoint = endpoint;
    this.#endpointRunner = new WebhookEndpointRunner(endpoint, runners);
  }

  /** Verify, record, and acknowledge one Calendly delivery; it answers 503 until run has replayed every binding's pending events and started receiving, so Calendly retries. */
  serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> { return this.#endpointRunner.serveHTTP(request, response); }

  /** Replay each binding's durable inbox and then deliver new events until the context ends or a binding fails; rejects with TriggerDeliveryAbortedError after the context ends. */
  run(context: TriggerContext): Promise<void> { return this.#endpointRunner.run(context); }

  /** How many bindings receive events; a readiness check can wait until it equals the number of routes. */
  runningSourceCount(): number { return this.#endpoint.runningSourceCount(); }
}

/**
 * Open connectionName and every route's binding from the loaded project and wrap each target in the
 * binding's durable project inbox, so an event is stored before Calendly receives 200. A route whose
 * binding is not configured or is invalid throws. The client options' logger also applies to the inboxes.
 */
export function newProjectInviteeEventReceivedEndpointRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectInviteeEventReceivedTriggerRoute[], options: CalendlyClientOptions = {}): InviteeEventReceivedEndpointRunner {
  if (routes.length === 0) throw new TypeError('Calendly inviteeEventReceived routes are required');
  const connection = newProjectConnection(project, connectionName, options);
  const client = calendlyClientOf(connection);
  const triggerName = InviteeEventReceivedTriggerDefinition.trigger.triggerName;
  const bindingNames = new Set<string>();
  const runners = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('Calendly inviteeEventReceived binding name and target are required');
    if (bindingNames.has(bindingName)) throw new TypeError(`Calendly inviteeEventReceived binding name "${bindingName}" is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    let configuration: InviteeEventReceivedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, InviteeEventReceivedTriggerConfigurationSchema); }
    catch (error) { throw new Error(`Calendly inviteeEventReceived binding "${bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { logger: client.triggerLogger, decodePayload: (_payloadJSON, payloadBytes) => InviteeEventSchema.parse(parseTriggerPayloadJSON(payloadBytes)) });
    return newInviteeEventReceivedTrigger({ connection, connectionName, bindingName, configuration, target });
  });
  return new InviteeEventReceivedEndpointRunner(webhookEndpoint(client, connection.reference), runners);
}

/** Each client's shared endpoint per connection, created on first use. */
const webhookEndpoints = new WeakMap<CalendlyClient, Map<string, WebhookEndpoint<Credentials, InviteeEvent>>>();

function webhookEndpoint(client: CalendlyClient, connection: ConnectionRef): WebhookEndpoint<Credentials, InviteeEvent> {
  let endpoints = webhookEndpoints.get(client);
  if (!endpoints) { endpoints = new Map(); webhookEndpoints.set(client, endpoints); }
  const key = `${connection.provider}\u0000${connection.name}`;
  let endpoint = endpoints.get(key);
  if (!endpoint) {
    endpoint = new WebhookEndpoint<Credentials, InviteeEvent>({
      connectorId: ConnectorID, triggerName: InviteeEventReceivedTriggerDefinition.trigger.triggerName, connection,
      credentials: client.credentials, credentialRefresh: client.refreshDriver, maxBodyBytes: client.webhookMaxBodyBytes,
      verifyRequest: async (request, credentials) => verifyInviteeWebhookRequest(client, request, credentials),
      decodeEvent: decodeInviteeWebhookRequest, now: client.now, logger: client.triggerLogger,
    });
    endpoints.set(key, endpoint);
  }
  return endpoint;
}

function calendlyClientOf(connection: Connection): CalendlyClient {
  if (!(connection?.operations instanceof CalendlyClient)) throw new TypeError('Calendly Triggers require a connection from newProjectConnection or newConnection with a CalendlyClient');
  return connection.operations;
}

function acceptsEvent(configuration: InviteeEventReceivedTriggerConfiguration, event: TriggerEvent<InviteeEvent>): boolean {
  const events = configuration.events ?? [];
  if (events.length > 0 && !events.includes(event.payload.event)) return false;
  return !configuration.eventTypeUri || configuration.eventTypeUri === (event.payload.scheduledEvent.eventTypeUri ?? '');
}

/** A missing signing key answers 503, so Calendly keeps retrying the delivery. */
function verifyInviteeWebhookRequest(client: CalendlyClient, request: WebhookRequest, credentials: Credentials): void {
  const signingKey = credentials.webhookSigningKey?.reveal() ?? '';
  if (!signingKey) throw new WebhookVerificationUnavailableError();
  const header = request.headers[signatureHeader];
  verifyCalendlyWebhookSignature(request.body, (Array.isArray(header) ? header[0] : header) ?? '', signingKey, request.receivedAt, client.webhookSignatureToleranceMs);
}

/** Require a fresh t= and a v1= equal to hex HMAC-SHA256("t.body"); the error never names which check failed. */
function verifyCalendlyWebhookSignature(body: Uint8Array, header: string, signingKey: string, now: Date, toleranceMs: number): void {
  const invalid = () => new Error('Calendly-Webhook-Signature is missing, malformed, stale, or does not match');
  let timestampText = '';
  const signatures: Buffer[] = [];
  for (const component of header.split(',')) {
    const trimmed = component.trim();
    const separator = trimmed.indexOf('=');
    if (separator < 0) continue;
    const key = trimmed.slice(0, separator), value = trimmed.slice(separator + 1);
    if (key === 't') timestampText = value;
    else if (key === 'v1' && /^[0-9a-fA-F]{64}$/.test(value)) signatures.push(Buffer.from(value, 'hex'));
  }
  const timestamp = /^[+-]?\d+$/.test(timestampText) ? Number(timestampText) : Number.NaN;
  if (!Number.isSafeInteger(timestamp) || timestamp <= 0 || signatures.length === 0) throw invalid();
  const ageMs = now.getTime() - timestamp * 1000;
  if (ageMs > toleranceMs || ageMs < -toleranceMs) throw invalid();
  const expected = createHmac('sha256', signingKey).update(`${timestampText}.`).update(body).digest();
  if (!signatures.some(signature => timingSafeEqual(signature, expected))) throw invalid();
}

/** Decode a verified delivery; another webhook event, such as routing_form_submission.created, is acknowledged without a record. */
function decodeInviteeWebhookRequest(request: WebhookRequest): TriggerEvent<InviteeEvent> | undefined {
  // Like Go's json.RawMessage payload, another event family's payload is acknowledged without being decoded.
  const envelope = parseObject(JSON.parse(Buffer.from(request.body).toString('utf8')));
  const event = stringField(envelope, 'event');
  const createdAtText = stringField(envelope, 'created_at');
  if (!event || envelope.payload === undefined) throw new TypeError('invalid Calendly webhook');
  if (event !== WebhookEventInviteeCreated && event !== WebhookEventInviteeCanceled) return undefined;
  const createdAt = rfc3339(createdAtText);
  const payload = parseObject(parseObject(parseCalendlyJSON(request.body)).payload);
  if (payload.scheduled_event == null) throw new TypeError('invalid Calendly webhook');
  const invitee = decodeInvitee(payload, stringField(payload, 'event'));
  const scheduledEvent = decodeEvent(payload.scheduled_event);
  const identifiers = inviteeURIPattern.exec(invitee.uri);
  if (!identifiers || scheduledEvent.uri !== invitee.scheduledEventUri) throw new TypeError('invalid Calendly webhook');
  return { id: `${event}:${identifiers[1]}:${identifiers[2]}`, occurredAt: new Date(createdAt), payload: { event, createdAt, invitee, scheduledEvent } };
}
