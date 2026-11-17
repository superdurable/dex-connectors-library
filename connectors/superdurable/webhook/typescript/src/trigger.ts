// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { IncomingMessage, ServerResponse } from 'node:http';
import { z } from 'zod';
import { mustTriggerBindingDefinition, newTrigger, type ConnectionRef, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition, type TriggerEvent, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { WebhookEndpoint, WebhookEndpointRunner } from '@superdurable/connectors-sdk/webhooktrigger';
import { WebhookClient, type WebhookClientOptions } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import type { Credentials } from './generated/models.js';
import { decodeInboundRequest, parsePointer, pointerValue, verifyInboundRequest, type WebhookRequestEvent } from './request.js';
import { newProjectConnection } from './project.js';

/** Filters the requests one binding records; empty accepts every verified request. */
export const RequestReceivedTriggerConfigurationSchema = z.strictObject({
  /** RFC 6901 pointer to a JSON or form value. */ matchPointer: z.string().optional(),
  /** Accepted string, number, or boolean text at matchPointer. */ matchValues: z.array(z.string()).nullable().optional(),
}).superRefine((configuration, context) => {
  if (!!configuration.matchPointer !== !!configuration.matchValues?.length)
    context.addIssue({ code: 'custom', message: 'requestReceived matchPointer and matchValues must be set together' });
  if (configuration.matchPointer) {
    try { parsePointer(configuration.matchPointer); }
    catch { context.addIssue({ code: 'custom', message: 'requestReceived matchPointer must be an RFC 6901 pointer' }); }
  }
});
/** Request filter for one binding. */
export type RequestReceivedTriggerConfiguration = z.infer<typeof RequestReceivedTriggerConfigurationSchema>;
/** Strict decoder for a persisted verified request. */
export const WebhookRequestEventSchema = z.strictObject({
  contentType: z.enum(['application/json', 'application/x-www-form-urlencoded']),
  headers: z.record(z.string(), z.string()).optional(), jsonBody: z.unknown().optional(),
  formBody: z.record(z.string(), z.array(z.string())).optional(), receivedAt: z.string(),
});
/** Stable requestReceived manifest Trigger definition. */
export const RequestReceivedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'requestReceived' },
  description: 'Receive one verified JSON or form-encoded webhook request, such as a form submission, with a stable event ID.',
};
/** Static application binding of requestReceived. */
export interface RequestReceivedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Trigger-specific Dex Web UI guidance. */ configurationUI: ConnectorConfigurationUI;
}
/** Declare a static binding for Dex Web and FDG tooling. */
export function defineRequestReceivedTriggerBinding(config: RequestReceivedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: RequestReceivedTriggerDefinition, ...config });
}
/** Bind one requestReceived source to an application target. */
export interface RequestReceivedTriggerConfig {
  /** Named Webhook connection. */ connection: Connection;
  /** Static connection name matching the connection. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Binding event filter. */ configuration: RequestReceivedTriggerConfiguration;
  /** Application delivery target. */ target: TriggerTarget<WebhookRequestEvent>;
}
/** Create a Trigger source on the connection's shared webhook endpoint. */
export function newRequestReceivedTrigger(config: RequestReceivedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError('Webhook Trigger connection name differs');
  const client = webhookClientOf(config.connection);
  const configuration = RequestReceivedTriggerConfigurationSchema.parse(config.configuration);
  const source = webhookEndpoint(client, config.connection.reference).newSource(event => acceptsEvent(configuration, event));
  return newTrigger({ definition: RequestReceivedTriggerDefinition,
    binding: { connection: config.connection.reference, trigger: RequestReceivedTriggerDefinition.trigger, name: config.bindingName },
    source, target: config.target, logger: client.triggerLogger });
}
/** Shared HTTP handler for one connection. */
export interface RequestReceivedWebhookHandler {
  /** Verify, persist, and acknowledge one delivery. */ serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void>;
  /** Number of active bindings; zero answers 503. */ runningSourceCount(): number;
}
/** Return the shared endpoint for mounting at a public HTTPS webhook URL. */
export function requestReceivedWebhookHandler(connection: Connection): RequestReceivedWebhookHandler {
  return webhookEndpoint(webhookClientOf(connection), connection.reference);
}
/** One configured binding and application target. */
export interface ProjectRequestReceivedTriggerRoute {
  /** Configured binding name. */ bindingName: string;
  /** Target reached through the binding's durable inbox. */ target: TriggerTarget<WebhookRequestEvent>;
}
/** Serve one endpoint while replaying and delivering all configured binding inboxes. */
export class RequestReceivedEndpointRunner {
  readonly #runner: WebhookEndpointRunner;
  readonly #endpoint: WebhookEndpoint<Credentials, WebhookRequestEvent>;
  /** @internal Construct after validating every route. */
  constructor(endpoint: WebhookEndpoint<Credentials, WebhookRequestEvent>, runners: readonly TriggerRunner[]) {
    this.#endpoint = endpoint; this.#runner = new WebhookEndpointRunner(endpoint, runners);
  }
  /** Verify, persist, and acknowledge one webhook delivery. */
  serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> { return this.#runner.serveHTTP(request, response); }
  /** Replay pending records, then deliver new events until aborted. */
  run(context: TriggerContext): Promise<void> { return this.#runner.run(context); }
  /** Report active bindings for readiness checks. */
  runningSourceCount(): number { return this.#endpoint.runningSourceCount(); }
}
/** Open a connection and wrap each configured binding target in its durable project inbox. */
export function newProjectRequestReceivedEndpointRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectRequestReceivedTriggerRoute[], options: WebhookClientOptions = {}): RequestReceivedEndpointRunner {
  if (!routes.length) throw new TypeError('Webhook requestReceived routes are required');
  const connection = newProjectConnection(project, connectionName, options);
  const client = webhookClientOf(connection);
  const names = new Set<string>();
  const runners = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('Webhook requestReceived binding name and target are required');
    if (names.has(bindingName)) throw new TypeError(`Webhook requestReceived binding name ${bindingName} is duplicated`);
    names.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName: 'requestReceived', bindingName };
    let configuration: RequestReceivedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, RequestReceivedTriggerConfigurationSchema); }
    catch (error) { throw new Error(`Webhook requestReceived binding "${bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, {
      logger: client.triggerLogger, decodePayload: (_payloadJSON, bytes) => WebhookRequestEventSchema.parse(parseTriggerPayloadJSON(bytes)),
    });
    return newRequestReceivedTrigger({ connection, connectionName, bindingName, configuration, target });
  });
  return new RequestReceivedEndpointRunner(webhookEndpoint(client, connection.reference), runners);
}
const endpoints = new WeakMap<WebhookClient, Map<string, WebhookEndpoint<Credentials, WebhookRequestEvent>>>();
function webhookEndpoint(client: WebhookClient, connection: ConnectionRef): WebhookEndpoint<Credentials, WebhookRequestEvent> {
  let byConnection = endpoints.get(client);
  if (!byConnection) { byConnection = new Map(); endpoints.set(client, byConnection); }
  const key = `${connection.provider}\u0000${connection.name}`;
  let endpoint = byConnection.get(key);
  if (!endpoint) {
    endpoint = new WebhookEndpoint<Credentials, WebhookRequestEvent>({
      connectorId: ConnectorID, triggerName: 'requestReceived', connection, credentials: client.credentials,
      maxBodyBytes: client.maxBodyBytes,
      verifyRequest: async (request, credentials) => verifyInboundRequest(request, credentials, client.inboundSettings),
      decodeEvent: request => decodeInboundRequest(request, client.inboundSettings), now: client.now, logger: client.triggerLogger,
    });
    byConnection.set(key, endpoint);
  }
  return endpoint;
}
function webhookClientOf(connection: Connection): WebhookClient {
  if (!(connection?.operations instanceof WebhookClient)) throw new TypeError('Webhook Trigger requires a WebhookClient connection');
  return connection.operations;
}
function acceptsEvent(configuration: RequestReceivedTriggerConfiguration, event: TriggerEvent<WebhookRequestEvent>): boolean {
  if (!configuration.matchPointer) return true;
  const value = pointerValue(parsePointer(configuration.matchPointer), event.payload.contentType === 'application/json' ? event.payload.jsonBody : event.payload.formBody);
  if (typeof value !== 'string' && typeof value !== 'number' && typeof value !== 'boolean') return false;
  return (configuration.matchValues ?? []).includes(String(value));
}
