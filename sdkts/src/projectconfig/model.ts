// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { isPrivateBrokerHost } from '../netpolicy.js';

/** Current secret-free project configuration schema shared with Dex Web. */
export const ConfigurationSchemaVersion = 'connectors.dex.dev/project-configuration/v1alpha3';
/** Local Dex Web connections file schema. */
export const LocalConnectionsSchemaVersion = 'connectors.dex.dev/local-connections/v1alpha1';

/** RFC3339 timestamp with at most nanosecond precision. */
export const RFC3339NanoSchema = z.iso.datetime({ offset: true }).refine(value => {
  const fraction = /\.(\d+)(?:Z|[+-]\d{2}:\d{2})$/.exec(value)?.[1];
  return fraction === undefined || fraction.length <= 9;
}, 'timestamp precision exceeds nanoseconds');

/** Fixed project and Live or Preview credential boundary. */
export const ScopeSchema = z.strictObject({ projectId: z.string().regex(/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$/), kind: z.enum(['live', 'preview']) });
/** Project ID and live or preview scope. */
export type Scope = z.infer<typeof ScopeSchema>;

/** Return the canonical relative storage prefix for a trusted scope. */
export function scopePrefix(scope: Scope): string {
  const parsed = ScopeSchema.parse(scope);
  return `projects/${parsed.projectId}/${parsed.kind}`;
}

/** Exact versioned private application secret reference. */
export const ApplicationSecretRefSchema = z.strictObject({ key: z.string().min(1), version: z.string().min(1), digest: z.string().regex(/^sha256:[a-f0-9]{64}$/) });
/** Immutable private application secret reference. */
export type ApplicationSecretRef = z.infer<typeof ApplicationSecretRefSchema>;

/** Ordinary or private application environment value. */
export const EnvironmentValueSchema = z.strictObject({ value: z.string().optional(), secretRef: ApplicationSecretRefSchema.optional() }).refine(value => (value.value === undefined) !== (value.secretRef === undefined));
/** Ordinary value or private secret reference. */
export type EnvironmentValue = z.infer<typeof EnvironmentValueSchema>;

/** Logical connection and manifest-validated ordinary settings. */
export const ConnectionConfigurationSchema = z.strictObject({
  connectorId: z.string().regex(/^[a-z][a-z0-9-]{1,62}$/), connectionName: z.string().min(1),
  modulePath: z.string().min(1), provider: z.string().min(1), authMethodId: z.string().optional(),
  sharedConnectionId: z.string().regex(/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/).optional(),
  configuration: z.record(z.string(), z.unknown()),
});
/** Ordinary connection configuration and optional shared identity. */
export type ConnectionConfiguration = z.infer<typeof ConnectionConfigurationSchema>;

/** Connector Trigger binding startup settings. */
export const TriggerConfigurationSchema = z.strictObject({
  connectorId: z.string().min(1), connectionName: z.string().min(1), triggerName: z.string().min(1),
  bindingName: z.string().min(1), configuration: z.record(z.string(), z.unknown()),
});
/** Trigger binding and ordinary configuration. */
export type TriggerConfiguration = z.infer<typeof TriggerConfigurationSchema>;

/** Exact Flow and Step use configuration. */
export const OperationConfigurationSchema = z.strictObject({
  connectorId: z.string().min(1), connectionName: z.string().min(1), operationId: z.string().min(1),
  flowType: z.string().min(1), stepType: z.string().min(1), configuration: z.record(z.string(), z.unknown()),
});
/** Operation configuration for one Flow and Step. */
export type OperationConfiguration = z.infer<typeof OperationConfigurationSchema>;

/** Scope-owned credential broker endpoint and exact scope token reference. */
export const CredentialBrokerConfigurationSchema = z.strictObject({ endpoint: z.url(), tokenSecretRef: ApplicationSecretRefSchema });
/** Shared credential broker endpoint and token reference. */
export type CredentialBrokerConfiguration = z.infer<typeof CredentialBrokerConfigurationSchema>;

/** Immutable ordinary project configuration snapshot. */
export const ConfigurationSchema = z.strictObject({
  schemaVersion: z.enum(['connectors.dex.dev/project-configuration/v1alpha2', ConfigurationSchemaVersion]),
  scope: ScopeSchema, revision: z.number().int().positive(), connections: z.array(ConnectionConfigurationSchema),
  triggerBindings: z.array(TriggerConfigurationSchema), operationConfigurations: z.array(OperationConfigurationSchema),
  environment: z.record(z.string(), EnvironmentValueSchema).optional(), credentialBroker: CredentialBrokerConfigurationSchema.optional(),
}).superRefine((configuration, context) => {
  const hasShared = configuration.connections.some(connection => connection.sharedConnectionId !== undefined);
  if (hasShared && !configuration.credentialBroker) context.addIssue({ code: 'custom', message: 'shared connections require a credential broker' });
  if (configuration.schemaVersion.endsWith('v1alpha2') && (hasShared || configuration.credentialBroker)) context.addIssue({ code: 'custom', message: 'shared connections require v1alpha3' });
  if (configuration.credentialBroker) {
    const broker = configuration.credentialBroker;
    const expectedPrefix = `${scopePrefix(configuration.scope)}/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/`;
    if (!broker.tokenSecretRef.key.startsWith(expectedPrefix) || !/^[a-f0-9]{32}$/.test(broker.tokenSecretRef.key.slice(expectedPrefix.length))) context.addIssue({ code: 'custom', message: 'broker token reference differs from scope' });
    const endpoint = new URL(broker.endpoint);
    if (!['http:', 'https:'].includes(endpoint.protocol) || endpoint.username || endpoint.password || endpoint.search || endpoint.hash || (endpoint.protocol === 'http:' && !isPrivateBrokerHost(endpoint.hostname))) context.addIssue({ code: 'custom', message: 'broker endpoint is invalid' });
  }
  const connections = configuration.connections.map(item => `${item.connectorId}\0${item.connectionName}`);
  const triggers = configuration.triggerBindings.map(item => `${item.connectorId}\0${item.connectionName}\0${item.triggerName}\0${item.bindingName}`);
  const operations = configuration.operationConfigurations.map(item => `${item.connectorId}\0${item.connectionName}\0${item.operationId}\0${item.flowType}\0${item.stepType}`);
  for (const keys of [connections, triggers, operations]) {
    if (new Set(keys).size !== keys.length) context.addIssue({ code: 'custom', message: 'duplicate configuration identity' });
  }
});
/** Versioned ordinary project configuration. */
export type Configuration = z.infer<typeof ConfigurationSchema>;

/** Exact immutable configuration object version and digest. */
export const SnapshotRefSchema = z.strictObject({ key: z.string().min(1), version: z.string().min(1), digest: z.string().regex(/^sha256:[a-f0-9]{64}$/), mediaType: z.literal('application/json') });
/** Exact immutable configuration snapshot reference. */
export type SnapshotRef = z.infer<typeof SnapshotRefSchema>;

/** Local Dex Web record; credential contents remain private. */
export const LocalConnectionSchema = z.object({
  connectorId: z.string().min(1), connectionName: z.string().min(1), modulePath: z.string().min(1), provider: z.string().min(1),
  authMethodId: z.string().optional(), authMethodIds: z.array(z.string()).nullable().optional(),
  configuration: z.record(z.string(), z.unknown()), credentials: z.record(z.string(), z.unknown()),
  credentialExpiresAt: RFC3339NanoSchema.nullable().optional(),
});
/** Private Dex Web connection record. */
export type LocalConnection = z.infer<typeof LocalConnectionSchema>;

/** Dex Web local file, allowing future additive members. */
export const LocalConnectionsFileSchema = z.object({
  schemaVersion: z.literal(LocalConnectionsSchemaVersion), connections: z.array(LocalConnectionSchema),
  triggerBindings: z.array(TriggerConfigurationSchema).optional(),
});
/** Private Dex Web local connections file. */
export type LocalConnectionsFile = z.infer<typeof LocalConnectionsFileSchema>;
