// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, timingSafeEqual } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import { mustTriggerBindingDefinition, newTrigger, type ConnectionRef, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition, type TriggerEvent, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { WebhookEndpoint, WebhookEndpointRunner, WebhookVerificationUnavailableError, type WebhookRequest } from '@superdurable/connectors-sdk/webhooktrigger';
import { z } from 'zod';
import { TypeformClient, type TypeformClientOptions } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { FormResponseSchema, type Credentials } from './generated/models.js';
import { decodeFormResponse } from './operations.js';
import { newProjectConnection } from './project.js';

const formIdPattern = /^[A-Za-z0-9_-]{1,128}$/;

/** Filter submissions to one form; blank accepts all forms at the endpoint. */
export const ResponseSubmittedTriggerConfigurationSchema = z.strictObject({ formId: z.string().optional() }).superRefine((configuration, context) => {
  if (configuration.formId && !formIdPattern.test(configuration.formId)) context.addIssue({ code: 'custom', message: 'formId must be a Typeform form ID' });
});
/** Configuration of one responseSubmitted binding. */
export type ResponseSubmittedTriggerConfiguration = z.infer<typeof ResponseSubmittedTriggerConfigurationSchema>;

/** Verified Typeform submission with typed answers and hidden fields. */
export const FormResponseEventSchema = z.object({ webhookEventId: z.string(), formId: z.string(), formTitle: z.string().optional(), response: FormResponseSchema });
/** Verified Typeform submission. */
export type FormResponseEvent = z.infer<typeof FormResponseEventSchema>;

/** Manifest definition of responseSubmitted. */
export const ResponseSubmittedTriggerDefinition: TriggerDefinition = { trigger: { connectorId: ConnectorID, triggerName: 'responseSubmitted' }, description: 'Receive one signed Typeform form_response webhook with typed answers.' };
/** Provider-derived form picker unit. */ export const UIUnitFormPicker = 'formPicker';
/** Form picker output port. */ export const UIFormPickerPortFormID = 'formId';

/** Static responseSubmitted binding shown in Dex Web. */
export interface ResponseSubmittedTriggerBindingConfig { /** Connection name. */ connectionName: string; /** Binding name. */ bindingName: string; /** Connector Studio units. */ configurationUI: ConnectorConfigurationUI }
/** Declare a static responseSubmitted binding. */
export function defineResponseSubmittedTriggerBinding(config: ResponseSubmittedTriggerBindingConfig): TriggerBindingDefinition { return mustTriggerBindingDefinition({ definition: ResponseSubmittedTriggerDefinition, ...config }); }

/** One responseSubmitted binding and its delivery target. */
export interface ResponseSubmittedTriggerConfig { /** Typeform connection. */ connection: Connection; /** Static connection name. */ connectionName: string; /** Static binding name. */ bindingName: string; /** Form filter. */ configuration: ResponseSubmittedTriggerConfiguration; /** Delivery target. */ target: TriggerTarget<FormResponseEvent> }
/** Create a responseSubmitted Trigger from a shared webhook endpoint. */
export function newResponseSubmittedTrigger(config: ResponseSubmittedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError('Typeform responseSubmitted connection name does not match its connection');
  const client = typeformClientOf(config.connection);
  const configuration = ResponseSubmittedTriggerConfigurationSchema.parse(config.configuration);
  const source = webhookEndpoint(client, config.connection.reference).newSource(event => !configuration.formId || configuration.formId === event.payload.formId);
  const definition = ResponseSubmittedTriggerDefinition;
  return newTrigger({ definition, binding: { connection: config.connection.reference, trigger: definition.trigger, name: config.bindingName }, source, target: config.target, logger: client.triggerLogger });
}

/** HTTP handler of one Typeform connection. */
export interface ResponseSubmittedWebhookHandler { /** Verify and acknowledge a delivery. */ serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void>; /** Running bindings. */ runningSourceCount(): number }
/** Return the endpoint shared by every responseSubmitted binding of a connection. */
export function responseSubmittedWebhookHandler(connection: Connection): ResponseSubmittedWebhookHandler { return webhookEndpoint(typeformClientOf(connection), connection.reference); }

/** One stored binding routed through its durable inbox. */
export interface ProjectResponseSubmittedTriggerRoute { /** Stored binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<FormResponseEvent> }
/** Serve and run Typeform responseSubmitted bindings. */
export class ResponseSubmittedEndpointRunner {
  readonly #runner: WebhookEndpointRunner;
  /** @internal Construct through newProjectResponseSubmittedEndpointRunner. */
  constructor(private readonly endpoint: WebhookEndpoint<Credentials, FormResponseEvent>, runners: readonly TriggerRunner[]) { this.#runner = new WebhookEndpointRunner(endpoint, runners); }
  /** Verify and acknowledge one delivery after its inbox record is durable. */
  serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> { return this.#runner.serveHTTP(request, response); }
  /** Replay durable inboxes and deliver events until stopped. */
  run(context: TriggerContext): Promise<void> { return this.#runner.run(context); }
  /** Number of bindings ready to receive. */
  runningSourceCount(): number { return this.endpoint.runningSourceCount(); }
}
/** Load stored bindings and wrap targets in durable project inboxes. */
export function newProjectResponseSubmittedEndpointRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectResponseSubmittedTriggerRoute[], options: TypeformClientOptions = {}): ResponseSubmittedEndpointRunner {
  if (routes.length === 0) throw new TypeError('Typeform responseSubmitted routes are required');
  const connection = newProjectConnection(project, connectionName, options);
  const client = typeformClientOf(connection);
  const triggerName = ResponseSubmittedTriggerDefinition.trigger.triggerName;
  const bindingNames = new Set<string>();
  const runners = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('Typeform responseSubmitted binding name and target are required');
    if (bindingNames.has(bindingName)) throw new TypeError(`Typeform responseSubmitted binding name "${bindingName}" is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    const configuration = decodeTriggerConfiguration(project.configuration, key, ResponseSubmittedTriggerConfigurationSchema);
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { logger: client.triggerLogger, decodePayload: (_json, bytes) => FormResponseEventSchema.parse(parseTriggerPayloadJSON(bytes)) });
    return newResponseSubmittedTrigger({ connection, connectionName, bindingName, configuration, target });
  });
  return new ResponseSubmittedEndpointRunner(webhookEndpoint(client, connection.reference), runners);
}

const endpoints = new WeakMap<TypeformClient, Map<string, WebhookEndpoint<Credentials, FormResponseEvent>>>();
function webhookEndpoint(client: TypeformClient, connection: ConnectionRef): WebhookEndpoint<Credentials, FormResponseEvent> {
  let byConnection = endpoints.get(client);
  if (!byConnection) { byConnection = new Map(); endpoints.set(client, byConnection); }
  const key = `${connection.provider}\u0000${connection.name}`;
  let endpoint = byConnection.get(key);
  if (!endpoint) {
    endpoint = new WebhookEndpoint<Credentials, FormResponseEvent>({ connectorId: ConnectorID, triggerName: 'responseSubmitted', connection, credentials: client.credentials, maxBodyBytes: client.webhookMaxBodyBytes, verifyRequest, decodeEvent, now: client.triggerNow, logger: client.triggerLogger });
    byConnection.set(key, endpoint);
  }
  return endpoint;
}
function typeformClientOf(connection: Connection): TypeformClient {
  if (!(connection?.operations instanceof TypeformClient)) throw new TypeError('Typeform Triggers require a TypeformClient connection');
  return connection.operations;
}
async function verifyRequest(request: WebhookRequest, credentials: Credentials): Promise<void> {
  const secret = credentials.webhookSecret?.reveal() ?? '';
  if (!secret) throw new WebhookVerificationUnavailableError();
  const raw = request.headers['typeform-signature'];
  const signatureText = (Array.isArray(raw) ? raw[0] : raw)?.trim() ?? '';
  if (!/^sha256=[A-Za-z0-9+/]{43}=$/.test(signatureText)) throw new TypeError('invalid Typeform-Signature');
  const signature = Buffer.from(signatureText.slice(7), 'base64');
  const expected = createHmac('sha256', secret).update(request.body).digest();
  if (signature.length !== expected.length || !timingSafeEqual(signature, expected)) throw new TypeError('Typeform-Signature does not match the body');
}
function decodeEvent(request: WebhookRequest): TriggerEvent<FormResponseEvent> | undefined {
  const wire = JSON.parse(Buffer.from(request.body).toString('utf8')) as unknown;
  if (!wire || typeof wire !== 'object' || Array.isArray(wire)) throw new TypeError('Typeform webhook body is not a JSON object');
  const envelope = wire as Record<string, unknown>;
  if (envelope.event_id != null && typeof envelope.event_id !== 'string') throw new TypeError('Typeform event_id is invalid');
  if (typeof envelope.event_type !== 'string' || !envelope.event_type) throw new TypeError('Typeform webhook event_type is required');
  if (envelope.event_type !== 'form_response') return undefined;
  if (!envelope.form_response || typeof envelope.form_response !== 'object' || Array.isArray(envelope.form_response)) throw new TypeError('Typeform form_response is required');
  const submission = envelope.form_response as Record<string, unknown>;
  const formId = submission.form_id;
  if (typeof formId !== 'string' || !formIdPattern.test(formId) || typeof submission.token !== 'string') throw new TypeError('Typeform response identity is invalid');
  let formTitle = '';
  const definitions = new Map<string, { ref: string; title: string }>();
  if (submission.definition != null) {
    if (typeof submission.definition !== 'object' || Array.isArray(submission.definition)) throw new TypeError('Typeform definition is invalid');
    const definition = submission.definition as Record<string, unknown>;
    if (definition.id != null && definition.id !== '' && definition.id !== formId) throw new TypeError('Typeform definition form ID differs');
    if (definition.title != null && typeof definition.title !== 'string') throw new TypeError('Typeform definition title is invalid');
    formTitle = definition.title as string ?? '';
    if (definition.fields != null && !Array.isArray(definition.fields)) throw new TypeError('Typeform definition fields are invalid');
    for (const item of definition.fields as unknown[] ?? []) {
      if (!item || typeof item !== 'object' || Array.isArray(item)) throw new TypeError('Typeform definition field is invalid');
      const field = item as Record<string, unknown>;
      if (typeof field.id !== 'string' || (field.ref != null && typeof field.ref !== 'string') || (field.title != null && typeof field.title !== 'string')) throw new TypeError('Typeform definition field is invalid');
      definitions.set(field.id, { ref: typeof field.ref === 'string' ? field.ref : '', title: typeof field.title === 'string' ? field.title : '' });
    }
  }
  const response = decodeFormResponse(submission);
  if (!response.submittedAt) throw new TypeError('Typeform submitted_at is required');
  for (const answer of response.answers ?? []) {
    const definition = definitions.get(answer.fieldId);
    if (definition) { answer.fieldTitle = definition.title; if (!answer.fieldRef && definition.ref) answer.fieldRef = definition.ref; }
  }
  const payload: FormResponseEvent = { webhookEventId: typeof envelope.event_id === 'string' ? envelope.event_id : '', formId, ...(formTitle ? { formTitle } : {}), response };
  return { id: `${formId}:${response.token}`, occurredAt: new Date(response.submittedAt), payload };
}
