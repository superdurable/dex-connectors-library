// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { IncomingMessage, ServerResponse } from 'node:http';
import { z } from 'zod';
import { mustTriggerBindingDefinition, newTrigger, type ConnectionRef, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition, type TriggerEvent, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { WebhookEndpoint, WebhookEndpointRunner } from '@superdurable/connectors-sdk/webhooktrigger';
import { LinearClient, type LinearClientOptions } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { IssueSummarySchema, type Credentials } from './generated/models.js';
import { decodeIssueWebhookRequest, verifyLinearWebhookRequest } from './webhook.js';
import { newProjectConnection } from './project.js';

const uuidPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
/** Filters the Issue events one binding records. Empty values accept every Issue event. */
export const IssueEventReceivedTriggerConfigurationSchema = z.strictObject({
  /** Distinct create, update, or remove actions; empty accepts all. */ actions: z.array(z.string()).nullable().optional(),
  /** Linear team UUID; blank accepts every team. */ teamId: z.string().optional(),
}).superRefine((configuration, context) => {
  const selected = configuration.actions ?? [];
  if (selected.some(action => !['create', 'update', 'remove'].includes(action)) || new Set(selected).size !== selected.length)
    context.addIssue({ code: 'custom', message: 'issueEventReceived actions must be distinct values from create, update, and remove' });
  if (configuration.teamId && !uuidPattern.test(configuration.teamId))
    context.addIssue({ code: 'custom', message: 'issueEventReceived teamId must be a Linear team UUID' });
});
/** Event filter for one binding. */
export type IssueEventReceivedTriggerConfiguration = z.infer<typeof IssueEventReceivedTriggerConfigurationSchema>;
/** Verified event payload excluding issue descriptions, previous values, and actor email. */
export const IssueEventSchema = z.strictObject({
  action: z.string(), occurredAt: z.string(), webhookId: z.string(), organizationId: z.string(),
  actor: z.strictObject({ id: z.string(), name: z.string().optional(), type: z.string().optional() }).optional(),
  issue: IssueSummarySchema.extend({ number: z.number().int(), projectId: z.string().optional(), cycleId: z.string().optional(), parentId: z.string().optional(), trashed: z.boolean().optional() }),
  updatedFields: z.array(z.string()).optional(), previousStateId: z.string().optional(),
});
/** One verified Linear Issue webhook. */
export type IssueEvent = z.infer<typeof IssueEventSchema>;
/** Stable issueEventReceived manifest Trigger definition. */
export const IssueEventReceivedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'issueEventReceived' },
  description: 'Receive a signed Linear Issue webhook for a created, updated, or removed issue, verified with its Linear-Signature and fresh webhookTimestamp; the event ID is the action, the issue UUID, and the issue updatedAt.',
};
/** Static application binding of issueEventReceived. */
export interface IssueEventReceivedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Trigger-specific Dex Web UI guidance. */ configurationUI: ConnectorConfigurationUI;
}
/** Declare a static binding for Dex Web and FDG tooling. */
export function defineIssueEventReceivedTriggerBinding(config: IssueEventReceivedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: IssueEventReceivedTriggerDefinition, ...config });
}
/** Bind one issueEventReceived source to an application target. */
export interface IssueEventReceivedTriggerConfig {
  /** Named Linear connection. */ connection: Connection;
  /** Static connection name matching the connection. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Binding event filter. */ configuration: IssueEventReceivedTriggerConfiguration;
  /** Application delivery target. */ target: TriggerTarget<IssueEvent>;
}
/** Create a Trigger source on the connection's shared webhook endpoint. */
export function newIssueEventReceivedTrigger(config: IssueEventReceivedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError('Linear Trigger connection name differs');
  const client = linearClientOf(config.connection);
  const configuration = IssueEventReceivedTriggerConfigurationSchema.parse(config.configuration);
  const source = webhookEndpoint(client, config.connection.reference).newSource(event => acceptsEvent(configuration, event));
  return newTrigger({ definition: IssueEventReceivedTriggerDefinition,
    binding: { connection: config.connection.reference, trigger: IssueEventReceivedTriggerDefinition.trigger, name: config.bindingName },
    source, target: config.target, logger: client.triggerLogger });
}
/** Shared HTTP handler for one connection. */
export interface IssueEventReceivedWebhookHandler {
  /** Verify, persist, and acknowledge one delivery. */ serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void>;
  /** Number of active bindings; zero answers 503. */ runningSourceCount(): number;
}
/** Return the shared endpoint for mounting at a public HTTPS webhook URL. */
export function issueEventReceivedWebhookHandler(connection: Connection): IssueEventReceivedWebhookHandler {
  return webhookEndpoint(linearClientOf(connection), connection.reference);
}
/** One configured binding and application target. */
export interface ProjectIssueEventReceivedTriggerRoute {
  /** Configured binding name. */ bindingName: string;
  /** Target reached through the binding's durable inbox. */ target: TriggerTarget<IssueEvent>;
}
/** Serve one endpoint while replaying and delivering all configured binding inboxes. */
export class IssueEventReceivedEndpointRunner {
  readonly #runner: WebhookEndpointRunner;
  readonly #endpoint: WebhookEndpoint<Credentials, IssueEvent>;
  /** @internal Construct after validating every route. */
  constructor(endpoint: WebhookEndpoint<Credentials, IssueEvent>, runners: readonly TriggerRunner[]) {
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
export function newProjectIssueEventReceivedEndpointRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectIssueEventReceivedTriggerRoute[], options: LinearClientOptions = {}): IssueEventReceivedEndpointRunner {
  if (!routes.length) throw new TypeError('Linear issueEventReceived routes are required');
  const connection = newProjectConnection(project, connectionName, options);
  const client = linearClientOf(connection);
  const names = new Set<string>();
  const runners = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('Linear issueEventReceived binding name and target are required');
    if (names.has(bindingName)) throw new TypeError(`Linear issueEventReceived binding name ${bindingName} is duplicated`);
    names.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName: 'issueEventReceived', bindingName };
    let configuration: IssueEventReceivedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, IssueEventReceivedTriggerConfigurationSchema); }
    catch (error) { throw new Error(`Linear issueEventReceived binding "${bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, {
      logger: client.triggerLogger, decodePayload: (_payloadJSON, bytes) => IssueEventSchema.parse(parseTriggerPayloadJSON(bytes)),
    });
    return newIssueEventReceivedTrigger({ connection, connectionName, bindingName, configuration, target });
  });
  return new IssueEventReceivedEndpointRunner(webhookEndpoint(client, connection.reference), runners);
}
const endpoints = new WeakMap<LinearClient, Map<string, WebhookEndpoint<Credentials, IssueEvent>>>();
function webhookEndpoint(client: LinearClient, connection: ConnectionRef): WebhookEndpoint<Credentials, IssueEvent> {
  let byConnection = endpoints.get(client);
  if (!byConnection) { byConnection = new Map(); endpoints.set(client, byConnection); }
  const key = `${connection.provider}\u0000${connection.name}`;
  let endpoint = byConnection.get(key);
  if (!endpoint) {
    endpoint = new WebhookEndpoint<Credentials, IssueEvent>({
      connectorId: ConnectorID, triggerName: 'issueEventReceived', connection, credentials: client.credentials,
      credentialRefresh: client.refreshDriver, maxBodyBytes: client.webhookMaxBodyBytes,
      verifyRequest: async (request, credentials) => verifyLinearWebhookRequest(request, credentials, client.webhookSignatureToleranceMs),
      decodeEvent: decodeIssueWebhookRequest, now: client.now, logger: client.triggerLogger,
    });
    byConnection.set(key, endpoint);
  }
  return endpoint;
}
function linearClientOf(connection: Connection): LinearClient {
  if (!(connection?.operations instanceof LinearClient)) throw new TypeError('Linear Trigger requires a LinearClient connection');
  return connection.operations;
}
function acceptsEvent(configuration: IssueEventReceivedTriggerConfiguration, event: TriggerEvent<IssueEvent>): boolean {
  if ((configuration.actions ?? []).length && !configuration.actions?.includes(event.payload.action)) return false;
  return !configuration.teamId || configuration.teamId.toLowerCase() === event.payload.issue.team.id.toLowerCase();
}
