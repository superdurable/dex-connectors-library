// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { QuickBooksClient } from './client.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { InvoiceChangeFetcher } from './invoice_change_fetcher.js';
import { InvoiceChangedEventSchema, InvoiceChangedTriggerConfigurationSchema, InvoiceChangedTriggerDefinition, type InvoiceChangedEvent, type InvoiceChangedTriggerConfiguration } from './trigger_definition.js';

const triggerName = InvoiceChangedTriggerDefinition.trigger.triggerName;

/** Binds one invoiceChanged binding to its application target and cursor. */
export interface InvoiceChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** The binding configuration; it has no fields today, so a binding watches every invoice. */ configuration?: InvoiceChangedTriggerConfiguration;
  /**
   * Routes each invoice change into the application. A durable target from newDurableTriggerTarget records each
   * change before the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<InvoiceChangedEvent>;
  /**
   * Stores the binding's watermark, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so every
   * process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore;
}

/** Configures every QuickBooks Trigger binding polled through one connection. */
export interface InvoiceTriggerRunnerConfig {
  /** A connection from newConnection with a QuickBooksClient or from newProjectConnection; a mock has no client. */ connection: Connection;
  /** The invoiceChanged bindings; at least one is required. */ invoiceChangedRoutes: InvoiceChangedTriggerRoute[];
}

function quickBooksClientOf(connection: Connection): QuickBooksClient {
  if (!connection) throw new TypeError('QuickBooks invoiceChanged connection is required');
  if (!(connection.operations instanceof QuickBooksClient)) throw new TypeError('QuickBooks invoiceChanged Triggers require a QuickBooksClient connection, not a mock');
  return connection.operations;
}

// A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
const pollSchedule = (client: QuickBooksClient): PollSchedule => ({ intervalMs: client.pollIntervalMs });

/**
 * Create one poll runner for the configured invoiceChanged bindings. run replays each durable target, records a
 * baseline for a binding without a cursor, then polls every binding each pollInterval with jitter, one QuickBooks
 * query of at most 100 invoices per request and at most ten pages per poll. Each poll rereads the 30 seconds before
 * the newest change delivered, so a write QuickBooks made visible after an earlier poll still arrives. A 429 pauses
 * every binding of the connection for Retry-After, or 60 seconds without it; other failures back off per binding. A
 * cursor for another company or environment, or a QuickBooks validation Fault for the watermark query, records a
 * new baseline and logs a WARN record; changes in that gap are not delivered. Delivery is at least once, and each
 * invoice version keeps one Trigger event ID. Throws for a missing or mock connection, no routes, a blank binding
 * name, a missing target, or an invalid configuration.
 */
export function newInvoiceTriggerRunner(config: InvoiceTriggerRunnerConfig): Runner {
  const client = quickBooksClientOf(config.connection);
  if (!config.invoiceChangedRoutes?.length) throw new TypeError('QuickBooks invoiceChanged routes are required');
  const reference = config.connection.reference;
  const pollers: Poller[] = config.invoiceChangedRoutes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('QuickBooks invoiceChanged binding name and target are required');
    parseInvoiceChangedTriggerConfiguration(route.configuration ?? {});
    return newBindingPoller({
      definition: InvoiceChangedTriggerDefinition,
      binding: { connection: reference, trigger: InvoiceChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(), fetcher: new InvoiceChangeFetcher(client, reference), target: route.target,
    });
  });
  return newRunner({ connection: reference, schedule: pollSchedule(client), pollers, logger: client.triggerLogger });
}

/** Validate an invoiceChanged configuration; throws for any field, since the binding has none. */
export function parseInvoiceChangedTriggerConfiguration(configuration: unknown): InvoiceChangedTriggerConfiguration {
  const parsed = InvoiceChangedTriggerConfigurationSchema.safeParse(configuration);
  if (!parsed.success) throw new TypeError(`quickbooks invoiceChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

/** Decode a replayed Trigger inbox payload that the Go or TypeScript connector recorded. */
export function decodeInvoiceChangedEventPayload(payloadJSON: string): InvoiceChangedEvent {
  return InvoiceChangedEventSchema.parse(JSON.parse(payloadJSON));
}

/** Selects one invoiceChanged binding of the project configuration. */
export interface ProjectInvoiceChangedTriggerRoute {
  /** The binding name. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<InvoiceChangedEvent>;
}

/** Selects QuickBooks Trigger bindings of the project configuration. */
export interface ProjectInvoiceTriggerRunnerConfig {
  /** The invoiceChanged bindings; at least one is required. */ invoiceChangedRoutes: ProjectInvoiceChangedTriggerRoute[];
}

/**
 * Create a durable runner for invoiceChanged bindings of the loaded project. connection is the project connection
 * from newProjectConnection(project, name); its client options, such as logger, also apply to the durable inboxes.
 * Each route records changes in its binding's project Trigger inbox before its watermark commits in the binding's
 * project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. A binding without
 * saved configuration is valid. Throws as newInvoiceTriggerRunner does and for an invalid saved configuration.
 */
export function newProjectInvoiceTriggerRunner(project: LoadedProject, connection: Connection, config: ProjectInvoiceTriggerRunnerConfig): Runner {
  if (!project) throw new TypeError('QuickBooks invoiceChanged Triggers require a loaded project');
  const client = quickBooksClientOf(connection);
  const invoiceChangedRoutes = (config.invoiceChangedRoutes ?? []).map((route): InvoiceChangedTriggerRoute => {
    if (!route?.target) throw new TypeError('QuickBooks invoiceChanged binding target is required');
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName, bindingName: route.bindingName };
    let configuration: InvoiceChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, InvoiceChangedTriggerConfigurationSchema); }
    catch (error) {
      if (!(error instanceof ObjectNotFoundError)) throw new TypeError(`quickbooks invoiceChanged binding ${JSON.stringify(route.bindingName)} configuration is invalid`, { cause: error });
      configuration = {};
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { logger: client.triggerLogger, decodePayload: decodeInvoiceChangedEventPayload });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newInvoiceTriggerRunner({ connection, invoiceChangedRoutes });
}

/** Configuration of one in-memory invoiceChanged Trigger. */
export interface InvoiceChangedTriggerConfig {
  /** A connection with a QuickBooksClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** The binding configuration; it has no fields today. */ configuration?: InvoiceChangedTriggerConfiguration;
  /** Routes accepted changes into the application. */ target: TriggerTarget<InvoiceChangedEvent>;
}

/**
 * Create one invoiceChanged Trigger with an in-memory cursor, as Go's generated NewInvoiceChangedTrigger. It has no
 * project storage, so it records a new baseline on every start and changes made while it is stopped are lost; use
 * newProjectInvoiceTriggerRunner for durable delivery. Throws when connectionName differs from the connection's
 * name, for a mock connection, or for an invalid configuration.
 */
export function newInvoiceChangedTrigger(config: InvoiceChangedTriggerConfig): TriggerRunner {
  const client = quickBooksClientOf(config.connection);
  const reference = config.connection.reference;
  if (config.connectionName !== reference.name) throw new TypeError(`quickbooks connector trigger connectionName ${JSON.stringify(config.connectionName)} must equal its connection's name ${JSON.stringify(reference.name)}`);
  parseInvoiceChangedTriggerConfiguration(config.configuration ?? {});
  const binding = { connection: reference, trigger: InvoiceChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<InvoiceChangedEvent>({
    definition: InvoiceChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(), fetcher: new InvoiceChangeFetcher(client, reference),
  }, pollSchedule(client), client.triggerLogger);
  return newTrigger({ definition: InvoiceChangedTriggerDefinition, binding, source, target: config.target, logger: client.triggerLogger });
}
