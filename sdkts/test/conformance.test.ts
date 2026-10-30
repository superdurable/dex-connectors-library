// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile, readdir } from 'node:fs/promises';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { z } from 'zod';
import { CallIDNamespace, CallIDSchema, ConnectionRefSchema, MutationDefinitionWireSchema, OperationRefSchema, QueryDefinitionWireSchema, QueryResultSchema, ReceiptSchema, defaultConnectorStepExecuteRetryPolicy, deriveCallID, retryPolicyToGoWire } from '../src/index.js';
import { FailureSchema } from '../src/errors.js';
import { BrokerAnswerSchema } from '../src/projectconfig/broker.js';
import { ConfigurationSchema, LocalConnectionsFileSchema, ScopeSchema, SnapshotRefSchema, scopePrefix } from '../src/projectconfig/model.js';
import { ConnectionRecordSchema, CredentialEnvelopeSchema } from '../src/projectconfig/environment.js';
import { ConfigurationStore, ObjectNotFoundError, ProjectObject, encodeGoCompatibleJSON, connectionHeadKey, digestBytes, type ObjectStore } from '../src/projectconfig/storage.js';
import { CallReportSchema, RecordedCallSchema, ReportDirectoryEnvironment, ReportFileName, ReportRowSchema } from '../src/connectormock/index.js';

async function fixture(name: string): Promise<unknown> {
  const path = process.env.CONFORMANCE_FIXTURE_DIR ? resolve(process.env.CONFORMANCE_FIXTURE_DIR, `${name}.json`) : new URL(`../../conformance/${name}.json`, import.meta.url);
  return JSON.parse(await readFile(path, 'utf8')) as unknown;
}

function roundTrip<T>(schema: z.ZodType<T>, value: unknown): T {
  const parsed = schema.parse(value);
  expect(JSON.parse(JSON.stringify(parsed))).toEqual(value);
  return parsed;
}

describe('sdkgo conformance fixtures', () => {
  it('reads every committed JSON fixture', async () => {
    const directory = process.env.CONFORMANCE_FIXTURE_DIR ? resolve(process.env.CONFORMANCE_FIXTURE_DIR) : new URL('../../conformance/', import.meta.url);
    const names = (await readdir(directory)).filter(name => name.endsWith('.json'));
    expect(names.sort()).toEqual(['connections.json', 'connectormock.json', 'credential-broker.json', 'failures.json', 'identity.json', 'project-environment.json', 'project-storage.json', 'results.json', 'retry-policy.json', 'single-dispatch-markers.json']);
    const fixtureFields: Record<string, string[]> = {
      identity: ['callIDNamespace', 'derivedCall', 'differentOperationCallId', 'differentStepCallId', 'fallbackCompletedReceipt', 'providerKey', 'source', 'unicodeDerivedCallId', 'unicodeSource'],
      results: ['mutationHappy', 'mutationOptionalFailure', 'mutationUncertain', 'nanosecondOffsetReceipt', 'queryHappy', 'queryNull', 'queryOptionalFailure', 'wholeSecondReceipt'],
      'retry-policy': ['InitialInterval', 'BackoffCoefficient', 'MaximumInterval', 'MaximumAttempts', 'TotalDuration'],
      connections: ['schemaVersion', 'connections', 'triggerBindings'],
      'credential-broker': ['connectionNotSelected', 'firstRequest', 'ready', 'reauthorizationRequired', 'refreshRequest'],
      'project-environment': ['credentialBrokerSecretName', 'hosted', 'localDexWeb', 'localObjectStoreOverrides'],
      'project-storage': ['applicationSecret', 'completedConnectionRecord', 'configurationSnapshotCanonicalJSON', 'configurationSnapshotDocument', 'connectionHeadKey', 'connectionKey', 'connectionRecord', 'connectionRecordCanonicalJSON', 'credentialEnvelope', 'credentialEnvelopeCanonicalJSON', 'credentialExchangeEnvelope', 'credentialExchangeEnvelopeCanonicalJSON', 'credentialObjectKey', 'emptyTriggerInboxDocument', 'exchangeResultKey', 'mutation', 'objectCAS', 'objectReference', 'scope', 'snapshot', 'triggerInboxDocument', 'triggerInboxHeadKey'],
      connectormock: ['emptyBranches', 'mutationDefinition', 'nullBranches', 'queryDefinition', 'recordedCall', 'replayedRecordedCall', 'report', 'reportDirectoryEnvironment', 'reportFile', 'reportFileName', 'retryRecordedCall', 'unscriptedRecordedCall'],
    };
    for (const name of names) {
      const fixtureName = name.slice(0, -5);
      const value = await fixture(fixtureName);
      if (fixtureName in fixtureFields) expect(Object.keys(z.record(z.string(), z.unknown()).parse(value)).sort()).toEqual(fixtureFields[fixtureName]?.sort());
      expect(JSON.parse(JSON.stringify(value))).toEqual(value);
    }
    expect(z.array(FailureSchema).parse(await fixture('failures'))).toHaveLength(13);
    expect(z.array(z.record(z.string(), z.unknown())).parse(await fixture('single-dispatch-markers'))).toHaveLength(4);
  });

  it('round-trips stable identity and derives the same UUID', async () => {
    const sourceSchema = z.strictObject({ flowId: z.string(), stepExecutionId: z.string(), connectorId: z.string(), operationId: z.string() });
    const values = z.strictObject({ callIDNamespace: CallIDSchema, source: sourceSchema, unicodeSource: sourceSchema,
      unicodeDerivedCallId: CallIDSchema, derivedCall: z.strictObject({ id: CallIDSchema, idempotencyKey: z.string(), connection: ConnectionRefSchema, operation: OperationRefSchema }),
      differentStepCallId: CallIDSchema, differentOperationCallId: CallIDSchema, fallbackCompletedReceipt: ReceiptSchema, providerKey: z.string() }).parse(await fixture('identity'));
    const { source, derivedCall } = values;
    expect(values.callIDNamespace).toBe(CallIDNamespace);
    expect(deriveCallID(source.flowId, source.stepExecutionId, derivedCall.operation)).toBe(derivedCall.id);
    expect(deriveCallID(source.flowId, 'step-execution-8', derivedCall.operation)).toBe(values.differentStepCallId);
    expect(deriveCallID(source.flowId, source.stepExecutionId, { connectorId: source.connectorId, operationId: 'getItem' })).toBe(values.differentOperationCallId);
    const unicode = values.unicodeSource;
    expect(deriveCallID(unicode.flowId, unicode.stepExecutionId, { connectorId: unicode.connectorId, operationId: unicode.operationId })).toBe(values.unicodeDerivedCallId);
    expect(Buffer.byteLength(unicode.flowId)).toBeGreaterThan(unicode.flowId.length);
    expect(values.providerKey).toBe(`${source.connectorId}/${derivedCall.id}`);
    expect(JSON.parse(JSON.stringify(values))).toEqual(await fixture('identity'));
  });

  it('round-trips every failure kind and terminal result', async () => {
    const failures = z.array(FailureSchema).parse(await fixture('failures'));
    expect(failures.length).toBeGreaterThan(0);
    expect(JSON.parse(JSON.stringify(failures))).toEqual(await fixture('failures'));
    const results = z.record(z.string(), z.unknown()).parse(await fixture('results'));
    for (const value of Object.values(results)) {
      if (value && typeof value === 'object' && 'branch' in value) roundTrip(QueryResultSchema(z.unknown()), value);
      else roundTrip(ReceiptSchema, value);
    }
    expect(JSON.parse(JSON.stringify(results))).toEqual(await fixture('results'));
  });

  it('encodes Dex retry nanoseconds exactly', async () => {
    expect(retryPolicyToGoWire(defaultConnectorStepExecuteRetryPolicy())).toEqual(await fixture('retry-policy'));
  });

  it('round-trips local connections and broker responses', async () => {
    const connections = roundTrip(LocalConnectionsFileSchema, await fixture('connections'));
    expect(connections.connections).toHaveLength(5);
    expect(connections.connections.map(connection => connection.authMethodIds)).toEqual([['apiKey'], ['oauth2', 'apiKey'], ['multipleSecrets'], [], null]);
    const withoutAuthMethods = { ...connections.connections[0] };
    delete withoutAuthMethods.authMethodIds;
    expect(LocalConnectionsFileSchema.shape.connections.element.parse(withoutAuthMethods).authMethodIds).toBeUndefined();
    const broker = z.record(z.string(), z.unknown()).parse(await fixture('credential-broker'));
    for (const name of ['ready', 'reauthorizationRequired', 'connectionNotSelected']) roundTrip(BrokerAnswerSchema, broker[name]);
    const requestSchema = z.object({ sharedConnectionId: z.string(), rejectedRevision: z.number().int().positive().optional() });
    roundTrip(requestSchema, broker.firstRequest);
    roundTrip(requestSchema, broker.refreshRequest);
  });

  it('checks project storage identities and environment selectors', async () => {
    const storage = roundTrip(z.looseObject({
      scope: ScopeSchema, connectionKey: z.strictObject({ connectorId: z.string(), connectionName: z.string() }),
      connectionHeadKey: z.string(), credentialObjectKey: z.string(), exchangeResultKey: z.string(), triggerInboxHeadKey: z.string(),
      snapshot: SnapshotRefSchema, applicationSecret: z.strictObject({ key: z.string(), version: z.string(), digest: z.string() }),
      objectCAS: z.strictObject({ key: z.string(), version: z.string(), etag: z.string() }),
      objectReference: z.strictObject({ key: z.string(), version: z.string(), digest: z.string() }),
      mutation: z.strictObject({ kind: z.string(), invocationId: z.string(), id: z.string(), fence: z.number(), baseRevision: z.number(), deadline: z.string(), resultKey: z.string() }),
      configurationSnapshotDocument: ConfigurationSchema, configurationSnapshotCanonicalJSON: z.string(),
      connectionRecord: ConnectionRecordSchema, completedConnectionRecord: ConnectionRecordSchema, connectionRecordCanonicalJSON: z.string(),
      credentialEnvelope: CredentialEnvelopeSchema, credentialEnvelopeCanonicalJSON: z.string(),
      credentialExchangeEnvelope: CredentialEnvelopeSchema, credentialExchangeEnvelopeCanonicalJSON: z.string(),
      emptyTriggerInboxDocument: z.strictObject({ schemaVersion: z.literal('connectors.dex.dev/project-trigger-inbox/v1alpha1'), events: z.array(z.unknown()) }),
      triggerInboxDocument: z.strictObject({ schemaVersion: z.literal('connectors.dex.dev/project-trigger-inbox/v1alpha1'), events: z.array(z.strictObject({ id: z.string(), event: z.record(z.string(), z.unknown()) })) }),
    }), await fixture('project-storage'));
    expect(connectionHeadKey(storage.scope, storage.connectionKey.connectorId, storage.connectionKey.connectionName)).toBe(storage.connectionHeadKey);
    expect(storage.snapshot.key).toBe(`${scopePrefix(storage.scope)}/configuration/head`);
    expect(storage.objectCAS.key).toBe(storage.connectionHeadKey);
    expect(storage.objectCAS.etag).toBe(`${digestBytes(Buffer.from(storage.connectionRecordCanonicalJSON)).slice(7)}-${storage.objectCAS.version}`);
    expect(storage.objectReference.key).toBe(storage.credentialObjectKey);
    expect(storage.credentialObjectKey).toBe(storage.connectionHeadKey.replace(/\/head$/, `/credentials/${storage.objectReference.digest}`));
    expect(storage.exchangeResultKey).toBe(storage.connectionHeadKey.replace(/\/head$/, `/exchange-results/${storage.mutation.id}`));
    expect(storage.mutation.resultKey).toBe(storage.exchangeResultKey);
    expect(storage.connectionRecord.mutation).toEqual(storage.mutation);
    expect(storage.completedConnectionRecord.status).toBe('READY');
    expect(storage.emptyTriggerInboxDocument.events).toEqual([]);
    expect(storage.triggerInboxDocument.events).toHaveLength(1);
    expect(storage.triggerInboxHeadKey).toBe(`${scopePrefix(storage.scope)}/trigger-inboxes/sample/${Buffer.from(storage.connectionKey.connectionName).toString('base64url')}/itemCreated/${Buffer.from('main / west').toString('base64url')}/head`);
    const snapshot = roundTrip(ConfigurationSchema, storage.configurationSnapshotDocument);
    const canonical = encodeGoCompatibleJSON(snapshot);
    expect(canonical).toBe(storage.configurationSnapshotCanonicalJSON);
    expect(digestBytes(Buffer.from(canonical))).toBe(storage.snapshot.digest);
    let written: Uint8Array | undefined;
    const objects: ObjectStore = {
      async readObject() { throw new ObjectNotFoundError(); },
      async createObject(key, contents) { written = contents; return new ProjectObject(key, storage.snapshot.version, 'etag', contents); },
      async compareAndSwapObject() { throw new Error('unexpected CAS'); },
    };
    await new ConfigurationStore(objects, storage.scope).writeConfiguration(0, { connections: snapshot.connections, triggerBindings: snapshot.triggerBindings, operationConfigurations: snapshot.operationConfigurations, environment: snapshot.environment, credentialBroker: snapshot.credentialBroker });
    expect(Buffer.from(written ?? [])).toEqual(Buffer.from(storage.configurationSnapshotCanonicalJSON));
    for (const [documentName, canonicalName] of ([
      ['connectionRecord', 'connectionRecordCanonicalJSON'],
      ['credentialEnvelope', 'credentialEnvelopeCanonicalJSON'],
      ['credentialExchangeEnvelope', 'credentialExchangeEnvelopeCanonicalJSON'],
    ] as const)) {
      expect(encodeGoCompatibleJSON(storage[documentName])).toBe(storage[canonicalName]);
    }
    expect(digestBytes(Buffer.from(storage.credentialEnvelopeCanonicalJSON)).slice(7)).toBe(storage.objectReference.digest);
    const canonicalZeroOffset = encodeGoCompatibleJSON({ ...storage.credentialEnvelope, expiresAt: '2026-10-29T12:30:45.120+00:00' });
    expect(canonicalZeroOffset).toBe(storage.credentialEnvelopeCanonicalJSON.replace('2026-10-29T12:30:45.123456789+08:00', '2026-10-29T12:30:45.12Z'));
    expect(encodeGoCompatibleJSON({ ...storage.credentialEnvelope, expiresAt: '2026-10-29T12:30:45.000Z' })).toContain('"expiresAt":"2026-10-29T12:30:45Z"');
    expect(encodeGoCompatibleJSON({ ...storage.credentialEnvelope, expiresAt: '2026-10-29T12:30:45-00:00' })).toContain('"expiresAt":"2026-10-29T12:30:45Z"');
    expect(encodeGoCompatibleJSON({ ...storage.credentialEnvelope, expiresAt: '2026-10-29T12:30:45.120+08:00' })).toContain('"expiresAt":"2026-10-29T12:30:45.12+08:00"');
    expect(encodeGoCompatibleJSON({ ...storage.connectionRecord, mutation: { ...storage.mutation, deadline: '2026-10-29T12:30:45+00:00' } })).toContain('"deadline":"2026-10-29T12:30:45Z"');
    expect(() => encodeGoCompatibleJSON({ ...storage.credentialEnvelope, expiresAt: '2026-10-29T12:30:45.1234567891Z' })).toThrow('at most nine');
    expect(CredentialEnvelopeSchema.safeParse({ ...storage.credentialEnvelope, expiresAt: '2026-10-29T12:30:45.1234567891Z' }).success).toBe(false);
    expect(ConnectionRecordSchema.safeParse({ ...storage.connectionRecord, expiresAt: '2026-10-29T12:30:45.1234567891Z' }).success).toBe(false);
    expect(digestBytes(Buffer.from(storage.credentialExchangeEnvelopeCanonicalJSON)).slice(7)).toBe(storage.completedConnectionRecord.credential?.digest);
    expect(storage.applicationSecret.key).toBe(snapshot.credentialBroker?.tokenSecretRef.key);
    const environment = roundTrip(z.looseObject({ localDexWeb: z.strictObject({ DEX_CONNECTOR_CONFIG_FILE: z.string() }), hosted: z.record(z.string(), z.string()), localObjectStoreOverrides: z.record(z.string(), z.string()) }), await fixture('project-environment'));
    expect(environment.hosted.DEX_PROJECT_CONFIG_KEY).toBe(storage.snapshot.key);
    expect(environment.credentialBrokerSecretName).toBe('DEX_CREDENTIAL_BROKER_TOKEN');
    expect(Object.keys(environment.hosted).sort()).toEqual(['DEX_PROJECT_CONFIG_DIGEST', 'DEX_PROJECT_CONFIG_KEY', 'DEX_PROJECT_CONFIG_VERSION', 'DEX_PROJECT_ID', 'DEX_PROJECT_SCOPE', 'DEX_PROJECT_STORAGE_BUCKET', 'DEX_PROJECT_STORAGE_KMS_KEY_ARN', 'DEX_PROJECT_STORAGE_PREFIX'].sort());
    expect(Object.keys(environment.localObjectStoreOverrides).sort()).toEqual(['DEX_PROJECT_ALLOW_LOCAL_STORAGE', 'DEX_PROJECT_STORAGE_ENDPOINT'].sort());
  });

  it('round-trips mock records and report rows', async () => {
    const mock = roundTrip(z.looseObject({
      mutationDefinition: MutationDefinitionWireSchema, queryDefinition: QueryDefinitionWireSchema,
      recordedCall: RecordedCallSchema, retryRecordedCall: RecordedCallSchema, replayedRecordedCall: RecordedCallSchema,
      unscriptedRecordedCall: RecordedCallSchema, report: ReportRowSchema, reportFile: CallReportSchema,
      emptyBranches: QueryDefinitionWireSchema, nullBranches: QueryDefinitionWireSchema,
      reportDirectoryEnvironment: z.string(), reportFileName: z.string(),
    }), await fixture('connectormock'));
    roundTrip(RecordedCallSchema, mock.recordedCall);
    roundTrip(RecordedCallSchema, mock.retryRecordedCall);
    roundTrip(RecordedCallSchema, mock.replayedRecordedCall);
    roundTrip(RecordedCallSchema, mock.unscriptedRecordedCall);
    roundTrip(ReportRowSchema, mock.report);
    roundTrip(CallReportSchema, { calls: [mock.report] });
    roundTrip(CallReportSchema, mock.reportFile);
    for (const name of ['emptyBranches', 'nullBranches'] as const) roundTrip(QueryDefinitionWireSchema, mock[name]);
    expect(mock.reportDirectoryEnvironment).toBe(ReportDirectoryEnvironment);
    expect(mock.reportFileName).toBe(ReportFileName);
  });
});
