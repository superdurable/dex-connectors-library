// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, newTrigger, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { LinkedInClient } from './client.js';
import { ProfileFetcher } from './profile_fetcher.js';
import type { ProfileChangedEvent, ProfileClaims } from './profile_change.js';

/** Manifest definition of profileChanged. */
export const ProfileChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'profileChanged' },
  description: 'Poll the connected member\'s own OpenID Connect UserInfo profile; the first poll records a baseline without delivering it.',
};

/** Static profileChanged binding declaration. */
export interface ProfileChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Optional Connector Studio controls; the Trigger has no fields. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare one static profileChanged binding for Dex Flow tooling. */
export function defineProfileChangedTriggerBinding(config: ProfileChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ProfileChangedTriggerDefinition, ...config });
}

/** One profileChanged delivery route. */
export interface ProfileChangedTriggerRoute {
  /** Stable binding name. */ bindingName: string;
  /** Routes each change to the application. */ target: TriggerTarget<ProfileChangedEvent>;
  /** Optional durable cursor; omitted uses memory. */ cursor?: CursorStore;
}

/** Runner logging options. */
export interface ProfileTriggerRunnerOptions { /** Receives metadata, never claims or credentials. */ logger?: TriggerLogger; }

/** Create pollers sharing one connection rate limit and a per-binding cursor. */
export function newProfileTriggerRunner(connection: Connection, routes: readonly ProfileChangedTriggerRoute[], options: ProfileTriggerRunnerOptions = {}): Runner {
  const client = linkedinClientOf(connection);
  if (!routes?.length) throw new TypeError('LinkedIn profileChanged routes are required');
  const pollers: Poller[] = routes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('LinkedIn profileChanged binding name and target are required');
    return newBindingPoller({
      definition: ProfileChangedTriggerDefinition,
      binding: { connection: connection.reference, trigger: ProfileChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new ProfileFetcher(client, connection.reference, ProfileChangedTriggerDefinition.trigger),
      target: route.target,
    });
  });
  return newRunner({ connection: connection.reference, schedule: { intervalMs: client.pollIntervalMs }, pollers, ...(options.logger ? { logger: options.logger } : {}) });
}

/** Saved project binding selected by name. */
export interface ProjectProfileChangedTriggerRoute { /** Saved binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<ProfileChangedEvent>; }

/** Create a durable profileChanged runner with a project inbox and cursor per binding. */
export function newProjectProfileTriggerRunner(project: LoadedProject, connection: Connection, routes: readonly ProjectProfileChangedTriggerRoute[], options: ProfileTriggerRunnerOptions = {}): Runner {
  if (!project) throw new TypeError('LinkedIn project is required');
  const durable = routes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('LinkedIn profileChanged binding name and target are required');
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName: 'profileChanged', bindingName: route.bindingName };
    const saved = project.configuration.triggerBindings.find(entry => entry.connectorId === key.connectorId && entry.connectionName === key.connectionName && entry.triggerName === key.triggerName && entry.bindingName === key.bindingName);
    if (saved && Object.keys(saved.configuration ?? {}).length) throw new TypeError('LinkedIn profileChanged configuration must be empty');
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeProfileChangedEventPayload, ...(options.logger ? { logger: options.logger } : {}) });
    return { bindingName: route.bindingName, target, cursor: project.pollCursorStore(key) };
  });
  return newProfileTriggerRunner(connection, durable, options);
}

/** Single in-memory profileChanged Trigger. */
export interface ProfileChangedTriggerConfig { /** Bound connection. */ connection: Connection; /** Static binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<ProfileChangedEvent>; /** Optional metadata logger. */ logger?: TriggerLogger; }

/** Create a single in-memory Trigger that seeds a new baseline on each start. */
export function newProfileChangedTrigger(config: ProfileChangedTriggerConfig): TriggerRunner {
  const client = linkedinClientOf(config.connection);
  if (!config.bindingName?.trim() || !config.target) throw new TypeError('LinkedIn profileChanged binding name and target are required');
  const binding = { connection: config.connection.reference, trigger: ProfileChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource({ definition: ProfileChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(), fetcher: new ProfileFetcher(client, config.connection.reference, ProfileChangedTriggerDefinition.trigger) }, { intervalMs: client.pollIntervalMs }, config.logger);
  return newTrigger({ definition: ProfileChangedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}

/** Decode a durable inbox payload recorded by a Go, Python, or TypeScript runner. */
export function decodeProfileChangedEventPayload(payloadJSON: string): ProfileChangedEvent {
  const value: unknown = JSON.parse(payloadJSON);
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('stored LinkedIn profileChanged payload is invalid');
  const event = value as Record<string, unknown>;
  const allowedFields = new Set(['name', 'givenName', 'familyName', 'email', 'emailVerified', 'pictureUrl', 'locale']);
  if (typeof event.subject !== 'string' || !event.subject || !isClaims(event.previous) || !isClaims(event.current) || event.previous.subject !== event.subject || event.current.subject !== event.subject || !Array.isArray(event.changedFields) || !event.changedFields.every(field => typeof field === 'string' && allowedFields.has(field))) throw new TypeError('stored LinkedIn profileChanged payload is invalid');
  return event as unknown as ProfileChangedEvent;
}

function isClaims(value: unknown): value is ProfileClaims {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const profile = value as Record<string, unknown>;
  return typeof profile.subject === 'string' && !!profile.subject && typeof profile.emailVerified === 'boolean' && ['name', 'givenName', 'familyName', 'email', 'pictureUrl', 'locale'].every(field => profile[field] === undefined || typeof profile[field] === 'string');
}
function linkedinClientOf(connection: Connection): LinkedInClient {
  if (!(connection?.operations instanceof LinkedInClient)) throw new TypeError('LinkedIn profileChanged requires a connection with a LinkedInClient');
  return connection.operations;
}
