// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, timingSafeEqual } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import { mustTriggerBindingDefinition, newTrigger, type ConnectionRef, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition, type TriggerEvent, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { WebhookEndpoint, WebhookEndpointRunner, WebhookVerificationUnavailableError, type WebhookRequest } from '@superdurable/connectors-sdk/webhooktrigger';
import { z } from 'zod';
import { ClickUpClient, type ClickUpClientOptions } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import type { Credentials } from './generated/models.js';
import { newProjectConnection } from './project.js';

/** Supported ClickUp task webhook events in Go's stable order. */
export const TaskEvents = ['taskCreated', 'taskUpdated', 'taskDeleted', 'taskPriorityUpdated', 'taskStatusUpdated', 'taskAssigneeUpdated', 'taskDueDateUpdated', 'taskTagUpdated', 'taskMoved', 'taskCommentPosted', 'taskCommentUpdated', 'taskTimeEstimateUpdated', 'taskTimeTrackedUpdated'] as const;

/** Event filter of one taskEvent binding; an empty list accepts every supported event. */
export const TaskEventTriggerConfigurationSchema = z.strictObject({ events: z.array(z.string()).nullable().optional() }).superRefine((configuration, context) => {
  const events = configuration.events ?? [];
  if (events.some(event => !TaskEvents.includes(event as typeof TaskEvents[number])) || new Set(events).size !== events.length)
    context.addIssue({ code: 'custom', message: 'ClickUp webhook events must be distinct supported task events' });
});
/** One taskEvent binding's event filter. */
export type TaskEventTriggerConfiguration = z.infer<typeof TaskEventTriggerConfigurationSchema>;

/** One ClickUp task history item, with status names and no changed values. */
export const TaskHistoryItemSchema = z.object({ id: z.string(), field: z.string(), occurredAt: z.string().optional(), userId: z.number().optional(), parentId: z.string().optional(), statusBefore: z.string().optional(), statusAfter: z.string().optional() });
/** One ClickUp task history item. */
export type TaskHistoryItem = z.infer<typeof TaskHistoryItemSchema>;
/** One verified task webhook, with an event ID stable across ClickUp redelivery. */
export const TaskEventSchema = z.object({ eventId: z.string(), webhookId: z.string(), event: z.string(), taskId: z.string(), historyItems: z.array(TaskHistoryItemSchema), occurredAt: z.string() });
/** One verified task webhook. */
export type TaskEvent = z.infer<typeof TaskEventSchema>;

/** Manifest definition of taskEvent. */
export const TaskEventTriggerDefinition: TriggerDefinition = { trigger: { connectorId: ConnectorID, triggerName: 'taskEvent' }, description: 'Receive one signed ClickUp task webhook event filtered by event name.' };
/** ClickUp event picker UI unit. */ export const UIUnitTaskEventPicker = 'taskEventPicker';
/** Event picker output port. */ export const UITaskEventPickerPortEvents = 'events';

/** Static taskEvent binding shown in Dex Web. */
export interface TaskEventTriggerBindingConfig { /** Connection name. */ connectionName: string; /** Binding name. */ bindingName: string; /** Connector Studio units. */ configurationUI: ConnectorConfigurationUI }
/** Declare a static taskEvent binding. */
export function defineTaskEventTriggerBinding(config: TaskEventTriggerBindingConfig): TriggerBindingDefinition { return mustTriggerBindingDefinition({ definition: TaskEventTriggerDefinition, ...config }); }

/** One taskEvent binding and its target. */
export interface TaskEventTriggerConfig { /** ClickUp connection. */ connection: Connection; /** Static connection name. */ connectionName: string; /** Static binding name. */ bindingName: string; /** Event filter. */ configuration: TaskEventTriggerConfiguration; /** Delivery target. */ target: TriggerTarget<TaskEvent> }

/** Create a taskEvent Trigger from a connection's shared webhook endpoint. */
export function newTaskEventTrigger(config: TaskEventTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError('ClickUp taskEvent connection name does not match its connection');
  const client = clickupClientOf(config.connection);
  const configuration = TaskEventTriggerConfigurationSchema.parse(config.configuration);
  const source = webhookEndpoint(client, config.connection.reference).newSource(event => !configuration.events?.length || configuration.events.includes(event.payload.event));
  const definition = TaskEventTriggerDefinition;
  return newTrigger({ definition, binding: { connection: config.connection.reference, trigger: definition.trigger, name: config.bindingName }, source, target: config.target, logger: client.triggerLogger });
}

/** HTTP handler of one ClickUp connection; it answers 503 while no binding receives events. */
export interface TaskEventWebhookHandler { /** Verify and acknowledge a delivery. */ serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void>; /** Running bindings. */ runningSourceCount(): number }
/** Return the endpoint shared by every taskEvent binding of a connection. */
export function taskEventWebhookHandler(connection: Connection): TaskEventWebhookHandler { return webhookEndpoint(clickupClientOf(connection), connection.reference); }

/** One stored taskEvent binding routed through its durable inbox. */
export interface ProjectTaskEventTriggerRoute { /** Stored binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<TaskEvent> }

/** Serve and run a connection's taskEvent bindings. */
export class TaskEventEndpointRunner {
  readonly #runner: WebhookEndpointRunner;
  /** @internal Construct through newProjectTaskEventEndpointRunner. */
  constructor(private readonly endpoint: WebhookEndpoint<Credentials, TaskEvent>, runners: readonly TriggerRunner[]) { this.#runner = new WebhookEndpointRunner(endpoint, runners); }
  /** Verify, record, and acknowledge one delivery, or answer 503 until pending events have replayed. */
  serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> { return this.#runner.serveHTTP(request, response); }
  /** Replay durable inboxes, then deliver new events until stopped. */
  run(context: TriggerContext): Promise<void> { return this.#runner.run(context); }
  /** Number of bindings ready to receive. */
  runningSourceCount(): number { return this.endpoint.runningSourceCount(); }
}

/** Load stored bindings and wrap every target in a durable project inbox before webhook acknowledgement. */
export function newProjectTaskEventEndpointRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectTaskEventTriggerRoute[], options: ClickUpClientOptions = {}): TaskEventEndpointRunner {
  if (routes.length === 0) throw new TypeError('ClickUp taskEvent routes are required');
  const connection = newProjectConnection(project, connectionName, options);
  const client = clickupClientOf(connection);
  const triggerName = TaskEventTriggerDefinition.trigger.triggerName;
  const bindingNames = new Set<string>();
  const runners = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('ClickUp taskEvent binding name and target are required');
    if (bindingNames.has(bindingName)) throw new TypeError(`ClickUp taskEvent binding name "${bindingName}" is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    const configuration = decodeTriggerConfiguration(project.configuration, key, TaskEventTriggerConfigurationSchema);
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { logger: client.triggerLogger, decodePayload: (_json, bytes) => TaskEventSchema.parse(parseTriggerPayloadJSON(bytes)) });
    return newTaskEventTrigger({ connection, connectionName, bindingName, configuration, target });
  });
  return new TaskEventEndpointRunner(webhookEndpoint(client, connection.reference), runners);
}

const endpoints = new WeakMap<ClickUpClient, Map<string, WebhookEndpoint<Credentials, TaskEvent>>>();
function webhookEndpoint(client: ClickUpClient, connection: ConnectionRef): WebhookEndpoint<Credentials, TaskEvent> {
  let byConnection = endpoints.get(client);
  if (!byConnection) { byConnection = new Map(); endpoints.set(client, byConnection); }
  const key = `${connection.provider}\u0000${connection.name}`;
  let endpoint = byConnection.get(key);
  if (!endpoint) {
    endpoint = new WebhookEndpoint<Credentials, TaskEvent>({ connectorId: ConnectorID, triggerName: 'taskEvent', connection, credentials: client.credentials, maxBodyBytes: client.webhookMaxBodyBytes, verifyRequest, decodeEvent, now: client.triggerNow, logger: client.triggerLogger });
    byConnection.set(key, endpoint);
  }
  return endpoint;
}

function clickupClientOf(connection: Connection): ClickUpClient {
  if (!(connection?.operations instanceof ClickUpClient)) throw new TypeError('ClickUp Triggers require a ClickUpClient connection');
  return connection.operations;
}

async function verifyRequest(request: WebhookRequest, credentials: Credentials): Promise<void> {
  const secret = credentials.webhookSecret?.reveal() ?? '';
  if (!secret) throw new WebhookVerificationUnavailableError();
  const header = request.headers['x-signature'];
  const signatureText = (Array.isArray(header) ? header[0] : header)?.trim() ?? '';
  if (!/^[0-9a-fA-F]{64}$/.test(signatureText)) throw new TypeError('invalid X-Signature');
  const expected = createHmac('sha256', secret).update(request.body).digest();
  if (!timingSafeEqual(Buffer.from(signatureText, 'hex'), expected)) throw new TypeError('X-Signature does not match the body');
}

function decodeEvent(request: WebhookRequest): TriggerEvent<TaskEvent> | undefined {
  const wire = JSON.parse(Buffer.from(request.body).toString('utf8')) as unknown;
  if (!wire || typeof wire !== 'object' || Array.isArray(wire)) throw new TypeError('ClickUp webhook body is not a JSON object');
  const fields = wire as Record<string, unknown>;
  const event = fields.event;
  const webhookId = fields.webhook_id;
  if (typeof event !== 'string' || !event || typeof webhookId !== 'string' || !isPrintableIdentifier(webhookId)) throw new TypeError('ClickUp webhook event has no event name or webhook_id');
  if (!TaskEvents.includes(event as typeof TaskEvents[number])) return undefined;
  const taskId = flexibleIdentifier(fields.task_id);
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(taskId)) throw new TypeError('ClickUp task event has no valid task_id');
  const rawHistory = fields.history_items;
  if (rawHistory != null && !Array.isArray(rawHistory)) throw new TypeError('ClickUp history items are invalid');
  const historyItems = (rawHistory as unknown[] | null | undefined ?? []).slice(0, 20).map(decodeHistoryItem);
  const occurredAt = historyItems[0]?.occurredAt ?? goTimeJSON(request.receivedAt);
  const eventId = `${webhookId}:${event}:${historyItems[0]?.id ?? taskId}`;
  return { id: eventId, occurredAt: new Date(occurredAt), payload: { eventId, webhookId, event, taskId, historyItems, occurredAt } };
}

function decodeHistoryItem(value: unknown): TaskHistoryItem {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('ClickUp history item is invalid');
  const wire = value as Record<string, unknown>;
  const id = flexibleIdentifier(wire.id);
  if (!isPrintableIdentifier(id)) throw new TypeError('ClickUp history item has no valid id');
  if (wire.field != null && typeof wire.field !== 'string') throw new TypeError('ClickUp history item field is invalid');
  const field = wire.field ?? '';
  const occurredAt = unixMilliseconds(wire.date);
  const user = wire.user && typeof wire.user === 'object' && !Array.isArray(wire.user) ? wire.user as Record<string, unknown> : {};
  const userId = Number(flexibleIdentifier(user.id));
  const statusBefore = field === 'status' ? statusName(wire.before) : '';
  const statusAfter = field === 'status' ? statusName(wire.after) : '';
  return { id, field, ...(occurredAt ? { occurredAt } : {}), ...(Number.isSafeInteger(userId) && userId > 0 ? { userId } : {}), ...(wire.parent_id != null ? { parentId: flexibleIdentifier(wire.parent_id) } : {}), ...(statusBefore ? { statusBefore } : {}), ...(statusAfter ? { statusAfter } : {}) };
}

function flexibleIdentifier(value: unknown): string {
  if (value == null) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' && Number.isSafeInteger(value)) return String(value);
  throw new TypeError('ClickUp identifier is not a string or number');
}

function unixMilliseconds(value: unknown): string | undefined {
  if (value == null || value === '') return undefined;
  const text = flexibleIdentifier(value);
  if (!/^\d+$/.test(text)) throw new TypeError('ClickUp time is not Unix milliseconds');
  const milliseconds = Number(text);
  if (!Number.isSafeInteger(milliseconds)) throw new TypeError('ClickUp time is not Unix milliseconds');
  return milliseconds > 0 ? goTimeJSON(new Date(milliseconds)) : undefined;
}

function goTimeJSON(value: Date): string { return value.toISOString().replace(/\.000Z$/, 'Z'); }
function statusName(value: unknown): string { return value && typeof value === 'object' && !Array.isArray(value) && typeof (value as Record<string, unknown>).status === 'string' ? (value as { status: string }).status : ''; }
function isPrintableIdentifier(value: string): boolean { return value.length > 0 && Buffer.byteLength(value) <= 64 && /^[!-~]+$/.test(value) && !value.includes(':'); }
