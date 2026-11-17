// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, timingSafeEqual } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import { mustTriggerBindingDefinition, newTrigger, type ConnectionRef, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition, type TriggerEvent, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { WebhookEndpoint, WebhookEndpointRunner, WebhookVerificationUnavailableError, type WebhookRequest } from '@superdurable/connectors-sdk/webhooktrigger';
import { z } from 'zod';
import { StripeClient, decodeCheckoutSession, type StripeClientOptions } from './client.js';
import { newProjectConnection, type Connection } from './connection.js';
import { ConnectorID } from './generated/definitions.js';
import { CheckoutSessionSchema } from './generated/models.js';
import type { Credentials } from './models.js';

/** Checkout Session event types decoded by the Go connector. */
export const CheckoutSessionEventTypes = ['checkout.session.completed', 'checkout.session.async_payment_succeeded', 'checkout.session.async_payment_failed', 'checkout.session.expired'] as const;

/** Event type filter of one checkoutSessionUpdated binding; empty accepts all four types. */
export const CheckoutSessionUpdatedTriggerConfigurationSchema = z.strictObject({ eventTypes: z.array(z.string()).nullable().optional() }).superRefine((configuration, context) => {
  const eventTypes = configuration.eventTypes ?? [];
  if (eventTypes.some(eventType => !CheckoutSessionEventTypes.includes(eventType as typeof CheckoutSessionEventTypes[number])) || new Set(eventTypes).size !== eventTypes.length)
    context.addIssue({ code: 'custom', message: 'Stripe Checkout Session event types must be distinct supported types' });
});
/** One binding's accepted Checkout Session event types. */
export type CheckoutSessionUpdatedTriggerConfiguration = z.infer<typeof CheckoutSessionUpdatedTriggerConfigurationSchema>;

/** Verified Stripe Checkout Session event with only connector-safe session fields. */
export const CheckoutSessionEventSchema = z.object({ type: z.string(), session: CheckoutSessionSchema });
/** One verified Stripe Checkout Session event. */
export type CheckoutSessionEvent = z.infer<typeof CheckoutSessionEventSchema>;

/** Manifest definition of checkoutSessionUpdated. */
export const CheckoutSessionUpdatedTriggerDefinition: TriggerDefinition = { trigger: { connectorId: ConnectorID, triggerName: 'checkoutSessionUpdated' }, description: 'Receive a verified Checkout Session completion, delayed-payment, or expiration event.' };

/** Static checkoutSessionUpdated binding shown in Dex Web. */
export interface CheckoutSessionUpdatedTriggerBindingConfig { /** Connection name. */ connectionName: string; /** Binding name. */ bindingName: string; /** Configuration units. */ configurationUI: ConnectorConfigurationUI }
/** Declare a static checkoutSessionUpdated binding. */
export function defineCheckoutSessionUpdatedTriggerBinding(config: CheckoutSessionUpdatedTriggerBindingConfig): TriggerBindingDefinition { return mustTriggerBindingDefinition({ definition: CheckoutSessionUpdatedTriggerDefinition, ...config }); }

/** One Checkout Session Trigger and its application target. */
export interface CheckoutSessionUpdatedTriggerConfig { /** Stripe connection. */ connection: Connection; /** Static connection name. */ connectionName: string; /** Static binding name. */ bindingName: string; /** Event type filter. */ configuration: CheckoutSessionUpdatedTriggerConfiguration; /** Delivery target. */ target: TriggerTarget<CheckoutSessionEvent> }

/** Create a Checkout Session Trigger fed by its connection's shared signed endpoint. */
export function newCheckoutSessionUpdatedTrigger(config: CheckoutSessionUpdatedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError('Stripe checkoutSessionUpdated connection name does not match its connection');
  const client = stripeClientOf(config.connection);
  const configuration = CheckoutSessionUpdatedTriggerConfigurationSchema.parse(config.configuration);
  const source = webhookEndpoint(client, config.connection.reference).newSource(event => !configuration.eventTypes?.length || configuration.eventTypes.includes(event.payload.type));
  const definition = CheckoutSessionUpdatedTriggerDefinition;
  return newTrigger({ definition, binding: { connection: config.connection.reference, trigger: definition.trigger, name: config.bindingName }, source, target: config.target, logger: client.triggerLogger });
}

/** Shared HTTP endpoint of one Stripe connection. */
export interface CheckoutSessionWebhookHandler { /** Verify and acknowledge one delivery. */ serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void>; /** Number of ready bindings. */ runningSourceCount(): number }
/** Return the shared Checkout Session endpoint; it answers 503 while no binding runs. */
export function checkoutSessionWebhookHandler(connection: Connection): CheckoutSessionWebhookHandler { return webhookEndpoint(stripeClientOf(connection), connection.reference); }

/** Stored checkoutSessionUpdated binding routed through its durable inbox. */
export interface ProjectCheckoutSessionUpdatedTriggerRoute { /** Stored binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<CheckoutSessionEvent> }

/** Serve and run one connection's Checkout Session Trigger bindings. */
export class CheckoutSessionWebhookRuntime {
  readonly #runner: WebhookEndpointRunner;
  /** @internal Construct through newProjectCheckoutSessionWebhookRuntime. */
  constructor(private readonly endpoint: WebhookEndpoint<Credentials, CheckoutSessionEvent>, runners: readonly TriggerRunner[]) { this.#runner = new WebhookEndpointRunner(endpoint, runners); }
  /** Verify, persist, and acknowledge a delivery, or answer 503 before inbox replay finishes. */
  serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> { return this.#runner.serveHTTP(request, response); }
  /** Replay pending events, then receive until stopped. */
  run(context: TriggerContext): Promise<void> { return this.#runner.run(context); }
  /** Number of bindings ready to receive. */
  runningSourceCount(): number { return this.endpoint.runningSourceCount(); }
}

/** Load stored bindings and put every target behind a durable inbox before Stripe receives 200. */
export function newProjectCheckoutSessionWebhookRuntime(project: LoadedProject, connectionName: string, routes: readonly ProjectCheckoutSessionUpdatedTriggerRoute[], options: StripeClientOptions = {}): CheckoutSessionWebhookRuntime {
  if (routes.length === 0) throw new TypeError('Stripe Checkout Session trigger routes are required');
  const connection = newProjectConnection(project, connectionName, options);
  const client = stripeClientOf(connection);
  const triggerName = CheckoutSessionUpdatedTriggerDefinition.trigger.triggerName;
  const bindingNames = new Set<string>();
  const runners = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('Stripe Checkout Session trigger binding name and target are required');
    if (bindingNames.has(bindingName)) throw new TypeError(`Stripe Checkout Session trigger binding name "${bindingName}" is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    const configuration = decodeTriggerConfiguration(project.configuration, key, CheckoutSessionUpdatedTriggerConfigurationSchema);
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { logger: client.triggerLogger, decodePayload: (_json, bytes) => CheckoutSessionEventSchema.parse(parseTriggerPayloadJSON(bytes)) });
    return newCheckoutSessionUpdatedTrigger({ connection, connectionName, bindingName, configuration, target });
  });
  return new CheckoutSessionWebhookRuntime(webhookEndpoint(client, connection.reference), runners);
}

const endpoints = new WeakMap<StripeClient, Map<string, WebhookEndpoint<Credentials, CheckoutSessionEvent>>>();
function webhookEndpoint(client: StripeClient, connection: ConnectionRef): WebhookEndpoint<Credentials, CheckoutSessionEvent> {
  let byConnection = endpoints.get(client);
  if (!byConnection) { byConnection = new Map(); endpoints.set(client, byConnection); }
  const key = `${connection.provider}\u0000${connection.name}`;
  let endpoint = byConnection.get(key);
  if (!endpoint) {
    endpoint = new WebhookEndpoint<Credentials, CheckoutSessionEvent>({ connectorId: ConnectorID, triggerName: 'checkoutSessionUpdated', connection, credentials: client.credentials,
      maxBodyBytes: client.webhookMaxBodyBytes, verifyRequest: async (request, credentials) => verifySignature(request, credentials, client.webhookSignatureToleranceMs),
      decodeEvent, now: client.triggerNow, logger: client.triggerLogger });
    byConnection.set(key, endpoint);
  }
  return endpoint;
}

function stripeClientOf(connection: Connection): StripeClient {
  if (!(connection?.operations instanceof StripeClient)) throw new TypeError('Stripe Triggers require a StripeClient connection');
  return connection.operations;
}

function verifySignature(request: WebhookRequest, credentials: Credentials, toleranceMs: number): void {
  const secret = credentials.authMethodId === 'api-key-with-webhook' ? credentials.webhookSecret?.reveal() ?? '' : '';
  if (!secret) throw new WebhookVerificationUnavailableError();
  const header = request.headers['stripe-signature'];
  const text = (Array.isArray(header) ? header[0] : header) ?? '';
  let timestamp = 0;
  const signatures: Buffer[] = [];
  for (const component of text.split(',')) {
    const trimmed = component.trim();
    const separator = trimmed.indexOf('=');
    if (separator < 0) continue;
    const name = trimmed.slice(0, separator);
    const value = trimmed.slice(separator + 1);
    if (name === 't') {
      if (!/^[+-]?\d+$/.test(value) || !Number.isSafeInteger(Number(value))) throw new TypeError('invalid Stripe signature timestamp');
      timestamp = Number(value);
    } else if (name === 'v1' && /^[0-9a-fA-F]{64}$/.test(value)) signatures.push(Buffer.from(value, 'hex'));
  }
  if (!timestamp || signatures.length === 0) throw new TypeError('Stripe signature is incomplete');
  if (Math.abs(request.receivedAt.getTime() - timestamp * 1000) > toleranceMs) throw new TypeError('Stripe signature timestamp is outside the tolerance');
  const expected = createHmac('sha256', secret).update(`${timestamp}.`).update(request.body).digest();
  if (!signatures.some(signature => timingSafeEqual(signature, expected))) throw new TypeError('Stripe signature does not match');
}

function decodeEvent(request: WebhookRequest): TriggerEvent<CheckoutSessionEvent> | undefined {
  const parsed = JSON.parse(Buffer.from(request.body).toString('utf8')) as unknown;
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new TypeError('Stripe event envelope is invalid');
  const envelope = parsed as Record<string, unknown>;
  if (envelope.object !== 'event' || typeof envelope.id !== 'string' || !envelope.id || typeof envelope.type !== 'string' || !envelope.type || !Number.isSafeInteger(envelope.created) || Number(envelope.created) <= 0)
    throw new TypeError('Stripe event envelope is invalid');
  const eventType = envelope.type;
  if (!CheckoutSessionEventTypes.includes(eventType as typeof CheckoutSessionEventTypes[number])) return undefined;
  if (!envelope.data || typeof envelope.data !== 'object' || Array.isArray(envelope.data)) throw new TypeError('Stripe event data is invalid');
  const session = decodeCheckoutSession(Buffer.from(JSON.stringify((envelope.data as Record<string, unknown>).object)));
  return { id: envelope.id, occurredAt: new Date(Number(envelope.created) * 1000), payload: { type: eventType, session } };
}
