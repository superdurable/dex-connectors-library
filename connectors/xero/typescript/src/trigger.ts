// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { XeroClient } from './client.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { InvoiceChangeFetcher, validateInvoiceChangedTriggerConfiguration } from './invoice_change_fetcher.js';
import { InvoiceChangedEventSchema, InvoiceChangedTriggerConfigurationSchema, InvoiceChangedTriggerDefinition, type InvoiceChangedEvent, type InvoiceChangedTriggerConfiguration } from './trigger_definition.js';

const triggerName = InvoiceChangedTriggerDefinition.trigger.triggerName;

/** Binds one invoiceChanged configuration to its application target and cursor. */
export interface InvoiceChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the invoice type and statuses; omitted receives every invoice and bill. */ configuration?: InvoiceChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A durable target from newDurableTriggerTarget records each change
   * before the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<InvoiceChangedEvent>;
  /**
   * Stores the binding's poll position, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so every
   * process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore;
}

/** Configures every Xero Trigger binding polled through one connection. */
export interface InvoiceTriggerRunnerConfig {
  /** A connection from newConnection with a XeroClient or from newProjectConnection; a mock has no client. */ connection: Connection;
  /** The invoiceChanged bindings; at least one is required. */ invoiceChangedRoutes: InvoiceChangedTriggerRoute[];
}

function xeroClientOf(connection: Connection): XeroClient {
  if (!connection) throw new TypeError('Xero invoiceChanged connection is required');
  if (!(connection.operations instanceof XeroClient)) throw new TypeError('Xero invoiceChanged Triggers require a XeroClient connection, not a mock');
  return connection.operations;
}

/** Validate an invoiceChanged configuration as Go's Validate does; throws a TypeError naming the problem. */
export function parseInvoiceChangedTriggerConfiguration(configuration: unknown): InvoiceChangedTriggerConfiguration {
  const parsed = InvoiceChangedTriggerConfigurationSchema.safeParse(configuration ?? {});
  if (!parsed.success) throw new TypeError(`xero invoiceChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  try { validateInvoiceChangedTriggerConfiguration(parsed.data); }
  catch (error) { throw new TypeError(`xero invoiceChanged configuration: ${error instanceof Error ? error.message : 'invalid'}`); }
  return parsed.data;
}

// A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
const pollSchedule = (client: XeroClient): PollSchedule => ({ intervalMs: client.pollIntervalMs });

/**
 * Create one poll runner for the configured invoiceChanged bindings. run replays each durable target, records a
 * baseline for a binding without a cursor, then polls every binding each pollInterval with jitter. A changed filter
 * or organisation records a new baseline and logs a WARN record; changes made between the last poll and the new
 * baseline are not delivered. A 429 for Xero's minute, concurrency, or daily limit pauses every binding of the
 * connection for Retry-After, or backs off exponentially without it; other failures back off per binding up to 15
 * minutes. Delivery is at least once, and each change keeps one Trigger event ID across refetches. Throws for a
 * missing or mock connection, no routes, a blank binding name, a missing target, or an invalid filter.
 */
export function newInvoiceTriggerRunner(config: InvoiceTriggerRunnerConfig): Runner {
  const client = xeroClientOf(config.connection);
  if (!config.invoiceChangedRoutes?.length) throw new TypeError('Xero invoiceChanged routes are required');
  const reference = config.connection.reference;
  const pollers: Poller[] = config.invoiceChangedRoutes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('Xero invoiceChanged binding name and target are required');
    const configuration = parseInvoiceChangedTriggerConfiguration(route.configuration);
    return newBindingPoller({
      definition: InvoiceChangedTriggerDefinition,
      binding: { connection: reference, trigger: InvoiceChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(), fetcher: new InvoiceChangeFetcher(client, reference, configuration), target: route.target,
    });
  });
  return newRunner({ connection: reference, schedule: pollSchedule(client), pollers, logger: client.triggerLogger });
}

/** Decode a replayed Trigger inbox payload that the Go or TypeScript connector recorded. */
export function decodeInvoiceChangedEventPayload(payloadJSON: string): InvoiceChangedEvent {
  return InvoiceChangedEventSchema.parse(JSON.parse(payloadJSON));
}

/** Selects one invoiceChanged binding saved in the project configuration. */
export interface ProjectInvoiceChangedTriggerRoute {
  /** The binding name. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<InvoiceChangedEvent>;
}

/** Selects Xero Trigger bindings saved in the project configuration. */
export interface ProjectInvoiceTriggerRunnerConfig {
  /** The invoiceChanged bindings; at least one is required. */ invoiceChangedRoutes: ProjectInvoiceChangedTriggerRoute[];
}

/**
 * Create a durable runner for invoiceChanged bindings of the loaded project. connection is the project connection
 * from newProjectConnection(project, name); its client options, such as logger, also apply to the durable inboxes.
 * Each binding's saved invoice filter is loaded (an unsaved binding receives every invoice and bill), and each route
 * records changes in its binding's project Trigger inbox before its position commits in the binding's project poll
 * cursor, so a restarted or replaced replica neither misses nor repeats a change. Throws as newInvoiceTriggerRunner
 * does and for an invalid saved configuration.
 */
export function newProjectInvoiceTriggerRunner(project: LoadedProject, connection: Connection, config: ProjectInvoiceTriggerRunnerConfig): Runner {
  if (!project) throw new TypeError('Xero invoiceChanged Triggers require a loaded project');
  const client = xeroClientOf(connection);
  const invoiceChangedRoutes = (config.invoiceChangedRoutes ?? []).map((route): InvoiceChangedTriggerRoute => {
    if (!route?.target) throw new TypeError(`Xero invoiceChanged binding ${JSON.stringify(route?.bindingName ?? '')} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName, bindingName: route.bindingName };
    let configuration: InvoiceChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, InvoiceChangedTriggerConfigurationSchema); }
    catch (error) {
      if (!(error instanceof ObjectNotFoundError)) throw new TypeError(`xero invoiceChanged binding ${JSON.stringify(route.bindingName)} configuration is invalid`, { cause: error });
      // An unsaved binding receives every invoice and bill, as a blank filter does.
      configuration = {};
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { logger: client.triggerLogger, decodePayload: decodeInvoiceChangedEventPayload });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newInvoiceTriggerRunner({ connection, invoiceChangedRoutes });
}

/** Configuration of one in-memory invoiceChanged Trigger. */
export interface InvoiceChangedTriggerConfig {
  /** A connection with a XeroClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Selects the invoice type and statuses; omitted receives every invoice and bill. */ configuration?: InvoiceChangedTriggerConfiguration;
  /** Routes accepted changes into the application. */ target: TriggerTarget<InvoiceChangedEvent>;
}

/**
 * Create one invoiceChanged Trigger with an in-memory cursor, as Go's generated NewInvoiceChangedTrigger. It has no
 * project storage, so it records a new baseline on every start and changes made while it is stopped are lost; use
 * newProjectInvoiceTriggerRunner for durable delivery. Throws when connectionName differs from the connection's
 * name, for a mock connection, or for an invalid filter.
 */
export function newInvoiceChangedTrigger(config: InvoiceChangedTriggerConfig): TriggerRunner {
  const client = xeroClientOf(config.connection);
  const reference = config.connection.reference;
  if (config.connectionName !== reference.name) throw new TypeError(`xero connector trigger connectionName ${JSON.stringify(config.connectionName)} must equal its connection's name ${JSON.stringify(reference.name)}`);
  const configuration = parseInvoiceChangedTriggerConfiguration(config.configuration);
  const binding = { connection: reference, trigger: InvoiceChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<InvoiceChangedEvent>({
    definition: InvoiceChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(), fetcher: new InvoiceChangeFetcher(client, reference, configuration),
  }, pollSchedule(client), client.triggerLogger);
  return newTrigger({ definition: InvoiceChangedTriggerDefinition, binding, source, target: config.target, logger: client.triggerLogger });
}
