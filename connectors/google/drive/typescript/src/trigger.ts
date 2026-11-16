// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingTriggerSource, newFeedMember, newFeedPoller, newMemoryCursorStore, newRunner, type CursorStore, type FeedMember, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { DriveClient, type DriveClientOptions } from './client.js';
import {
  BindingFileEventFetcher, FileChangeFetcher, FileChangesFeedName, FileCreatedTriggerName, FileTrashedTriggerName, FileUpdatedTriggerName,
  decodeFileEventPayload, fileChangeSelector, validateFileTriggerConfiguration,
  type FileChange, type FileEvent, type FileTriggerConfiguration,
} from './file_changes.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { newProjectConnection } from './project.js';

/** Folder picker UI unit, which stores a Drive folder ID and its display name. */ export const UIUnitFolderPicker = 'folderPicker';
/** Folder picker output port with the Drive folder ID. */ export const UIFolderPickerPortFolderID = 'folderId';
/** Folder picker output port with the listed folder's display name. */ export const UIFolderPickerPortFolderName = 'folderName';
/** MIME type picker UI unit, which stores one lowercase type/subtype. */ export const UIUnitMimeTypePicker = 'mimeTypePicker';
/** MIME type picker output port. */ export const UIMimeTypePickerPortMimeType = 'mimeType';

/** Manifest definition of fileCreated. */
export const FileCreatedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: FileCreatedTriggerName },
  description: 'Receive a Drive file created after the Trigger first started, optionally only in one folder or of one MIME type.',
};
/** Manifest definition of fileUpdated. */
export const FileUpdatedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: FileUpdatedTriggerName },
  description: 'Receive an existing Drive file whose modified time advanced, optionally only in one folder or of one MIME type.',
};
/** Manifest definition of fileTrashed. */
export const FileTrashedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: FileTrashedTriggerName },
  description: 'Receive a Drive file moved to the trash, optionally only in one folder or of one MIME type.',
};
const triggerDefinitions: Record<string, TriggerDefinition> = {
  [FileCreatedTriggerName]: FileCreatedTriggerDefinition, [FileUpdatedTriggerName]: FileUpdatedTriggerDefinition, [FileTrashedTriggerName]: FileTrashedTriggerDefinition,
};

/**
 * Decodes one saved file Trigger binding configuration with Go's strict JSON rules: unknown members and
 * wrong types are rejected, and null means blank. It does not validate the values; the runners do.
 */
export const FileTriggerConfigurationSchema: z.ZodType<FileTriggerConfiguration> = z.strictObject({
  folderId: z.string().nullable().optional(), folderName: z.string().nullable().optional(), mimeType: z.string().nullable().optional(),
}).transform(saved => ({
  ...(saved.folderId ? { folderId: saved.folderId } : {}), ...(saved.folderName ? { folderName: saved.folderName } : {}), ...(saved.mimeType ? { mimeType: saved.mimeType } : {}),
}));

/** One static application binding of a file Trigger. */
export interface FileTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the folderPicker and mimeTypePicker units. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static fileCreated binding for Flow Definition tooling and Dex Web. */
export function defineFileCreatedTriggerBinding(config: FileTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: FileCreatedTriggerDefinition, ...config });
}
/** Declare a static fileUpdated binding for Flow Definition tooling and Dex Web. */
export function defineFileUpdatedTriggerBinding(config: FileTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: FileUpdatedTriggerDefinition, ...config });
}
/** Declare a static fileTrashed binding for Flow Definition tooling and Dex Web. */
export function defineFileTrashedTriggerBinding(config: FileTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: FileTrashedTriggerDefinition, ...config });
}

/** Binds one fileCreated, fileUpdated, or fileTrashed binding to its application target. */
export interface FileTriggerRoute {
  /** Names this configured Trigger binding; it is unique per Trigger. */ bindingName: string;
  /** Filters the files this binding receives; omitted matches every file. */ configuration?: FileTriggerConfiguration;
  /** Routes accepted events into the application, such as newDexFlowTriggerTarget. */ target: TriggerTarget<FileEvent>;
}

/** Configures every file Trigger binding of one connection on one shared poller. */
export interface FileTriggerRunnerConfig {
  /** The Drive connection from newProjectConnection or newConnection with a DriveClient; a mock cannot poll. */ connection: Connection;
  /**
   * Stores the shared changes.list position. Use newMemoryCursorStore in tests; newProjectFileTriggerRunner
   * uses the project's durable feed cursor.
   */
  cursor: CursorStore;
  /** Receive files created after the cursor was first seeded. */ fileCreatedRoutes?: FileTriggerRoute[];
  /** Receive existing files whose modified time advanced. */ fileUpdatedRoutes?: FileTriggerRoute[];
  /** Receive files moved to the trash. */ fileTrashedRoutes?: FileTriggerRoute[];
}

/**
 * Reads the connection's Drive change log once per poll for every file Trigger binding and delivers each
 * binding's matching events in change order.
 */
export class FileTriggerRunner {
  readonly #runner: Runner;
  /** @internal Use newFileTriggerRunner or newProjectFileTriggerRunner. */
  constructor(runner: Runner) { this.#runner = runner; }
  /**
   * Replay every durable target, seed the cursor at Drive's current position on first start without
   * delivering existing files, and then poll until context.signal aborts. Rejects with
   * TriggerDeliveryAbortedError after the abort, or with an error for a stored cursor document that can never
   * be decoded. Delivery is at least once with stable event IDs, so a Dex Flow target starts one Flow per event.
   */
  run(context: TriggerContext): Promise<void> { return this.#runner.run(context); }
}

function driveClientOf(connection: Connection): DriveClient {
  const client = (connection?.operations as unknown);
  if (!(client instanceof DriveClient)) throw new TypeError('Google Drive file Triggers require a connection built from a DriveClient');
  return client;
}

/**
 * Validate config and build one runner with one shared changes.list feed; it reads and writes nothing until
 * run. Throws for a mock connection, a missing cursor or target, no routes, a duplicated binding name within
 * one Trigger, or an invalid configuration. One binding name may serve several Triggers.
 */
export function newFileTriggerRunner(config: FileTriggerRunnerConfig): FileTriggerRunner {
  const client = driveClientOf(config.connection);
  if (!config.cursor) throw new TypeError('Google Drive file Trigger cursor store is required');
  const reference = config.connection.reference;
  const members: FeedMember<FileChange>[] = [];
  for (const [triggerName, routes] of [
    [FileCreatedTriggerName, config.fileCreatedRoutes], [FileUpdatedTriggerName, config.fileUpdatedRoutes], [FileTrashedTriggerName, config.fileTrashedRoutes],
  ] as const) {
    for (const route of routes ?? []) members.push(newFileFeedMember(config.connection, triggerName, route));
  }
  if (members.length === 0) throw new TypeError('Google Drive file Trigger routes are required');
  const poller = newFeedPoller({ name: FileChangesFeedName, cursor: config.cursor, fetcher: new FileChangeFetcher(client, reference, FileChangesFeedName), members });
  return new FileTriggerRunner(newRunner({ connection: reference, schedule: { intervalMs: client.pollIntervalMs }, pollers: [poller], logger: client.triggerLogger }));
}

function newFileFeedMember(connection: Connection, triggerName: string, route: FileTriggerRoute): FeedMember<FileChange> {
  if (!route?.bindingName?.trim() || !route.target) throw new TypeError('Google Drive file Trigger binding name and target are required');
  const configuration = route.configuration ?? {};
  try { validateFileTriggerConfiguration(configuration); }
  catch (error) { throw new TypeError(`google-drive ${triggerName} binding ${JSON.stringify(route.bindingName)}: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
  const definition = triggerDefinitions[triggerName]!;
  return newFeedMember(definition, { connection: connection.reference, trigger: definition.trigger, name: route.bindingName }, fileChangeSelector(triggerName, configuration), route.target);
}

/** Binds one file Trigger binding saved in the project configuration to its target. */
export interface ProjectFileTriggerRoute {
  /** Names the saved Trigger binding. */ bindingName: string;
  /** Routes accepted events into the application. */ target: TriggerTarget<FileEvent>;
}

/** Selects file Trigger bindings saved in the project configuration. */
export interface ProjectFileTriggerRunnerConfig {
  /** Select saved fileCreated bindings. */ fileCreatedRoutes?: ProjectFileTriggerRoute[];
  /** Select saved fileUpdated bindings. */ fileUpdatedRoutes?: ProjectFileTriggerRoute[];
  /** Select saved fileTrashed bindings. */ fileTrashedRoutes?: ProjectFileTriggerRoute[];
}

/**
 * Open connectionName from the loaded project configuration, decode each selected binding's saved
 * configuration, and build one durable runner. Each route records events in its binding's project Trigger
 * inbox before the shared feed cursor, stored beside the inboxes, commits past them, so a restarted or
 * replaced replica neither misses nor replays a change. The cursor document and inbox payloads are the Go
 * connector's, so Go, Python, and TypeScript replicas can share them. options.triggerLogger also receives the
 * inboxes' records. Throws for a binding without saved configuration or an invalid one.
 */
export function newProjectFileTriggerRunner(project: LoadedProject, connectionName: string, config: ProjectFileTriggerRunnerConfig, options: DriveClientOptions = {}): FileTriggerRunner {
  if (!project || !config) throw new TypeError('Google Drive file Trigger runner requires the loaded project and routes');
  const connection = newProjectConnection(project, connectionName, options);
  const resolve = (triggerName: string, routes: ProjectFileTriggerRoute[] | undefined): FileTriggerRoute[] => (routes ?? []).map(route => {
    if (!route?.target) throw new TypeError('Google Drive file Trigger binding name and target are required');
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName: route.bindingName };
    let configuration: FileTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, FileTriggerConfigurationSchema); }
    catch (error) { throw new Error(`google-drive ${triggerName} binding ${JSON.stringify(route.bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { logger: options.triggerLogger, decodePayload: decodeFileEventPayload });
    return { bindingName: route.bindingName, configuration, target };
  });
  return newFileTriggerRunner({
    connection, cursor: project.pollFeedCursorStore({ connectorId: ConnectorID, connectionName, feedName: FileChangesFeedName }),
    fileCreatedRoutes: resolve(FileCreatedTriggerName, config.fileCreatedRoutes),
    fileUpdatedRoutes: resolve(FileUpdatedTriggerName, config.fileUpdatedRoutes),
    fileTrashedRoutes: resolve(FileTrashedTriggerName, config.fileTrashedRoutes),
  });
}

/** Configuration of one file Trigger made by newFileCreatedTrigger, newFileUpdatedTrigger, or newFileTrashedTrigger. */
export interface FileTriggerConfig {
  /** Drive connection from newProjectConnection or newConnection with a DriveClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Folder and MIME type filter; the empty configuration matches every file. */ configuration: FileTriggerConfiguration;
  /** Routes accepted events into the application. */ target: TriggerTarget<FileEvent>;
}

function newStandaloneFileTrigger(triggerName: string, config: FileTriggerConfig): TriggerRunner {
  const client = driveClientOf(config.connection);
  const reference = config.connection.reference;
  if (config.connectionName !== reference.name) throw new TypeError(`google-drive connector trigger connectionName ${JSON.stringify(config.connectionName)} must equal its connection's name ${JSON.stringify(reference.name)}`);
  validateFileTriggerConfiguration(config.configuration);
  const definition = triggerDefinitions[triggerName]!;
  const source = newBindingTriggerSource<FileEvent>({
    definition,
    // A per-Trigger poller never learns its binding name; this one appears only in logs and the in-memory cursor.
    binding: { connection: reference, trigger: definition.trigger, name: 'standalone' },
    cursor: newMemoryCursorStore(),
    fetcher: new BindingFileEventFetcher(new FileChangeFetcher(client, reference, triggerName), fileChangeSelector(triggerName, config.configuration)),
  }, { intervalMs: client.pollIntervalMs });
  return newTrigger({ definition, binding: { connection: reference, trigger: definition.trigger, name: config.bindingName }, source, target: config.target });
}

/**
 * Poll changes.list for one fileCreated binding with an in-memory cursor, so a restart reseeds and misses
 * files created while stopped; newProjectFileTriggerRunner is durable and shares one poll among bindings.
 * Throws for a mock connection, a connectionName that differs from the connection's, or an invalid configuration.
 */
export function newFileCreatedTrigger(config: FileTriggerConfig): TriggerRunner { return newStandaloneFileTrigger(FileCreatedTriggerName, config); }
/** Poll changes.list for one fileUpdated binding with an in-memory cursor, as newFileCreatedTrigger does. */
export function newFileUpdatedTrigger(config: FileTriggerConfig): TriggerRunner { return newStandaloneFileTrigger(FileUpdatedTriggerName, config); }
/** Poll changes.list for one fileTrashed binding with an in-memory cursor, as newFileCreatedTrigger does. */
export function newFileTrashedTrigger(config: FileTriggerConfig): TriggerRunner { return newStandaloneFileTrigger(FileTrashedTriggerName, config); }
