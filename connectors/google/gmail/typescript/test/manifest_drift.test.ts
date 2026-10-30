// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile } from 'node:fs/promises';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { z } from 'zod';
import { GetMessageDefinition, ReplyToMessageDefinition, SendMessageDefinition } from '../src/definitions.js';
import { GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID } from '../src/models.js';
import * as gmailmock from '../src/mock/index.js';

const FailureSchema = z.object({ kind: z.string(), message: z.string() });
const ManifestSchema = z.object({
  spec: z.object({
    languages: z.object({ typescript: z.object({ version: z.string() }) }),
    auth: z.object({ refreshable: z.boolean(), defaultMethod: z.string(), methods: z.array(z.object({
      id: z.string(), type: z.string(), fields: z.array(z.object({ name: z.string() })),
      oauth2: z.object({ tokenEndpoint: z.string(), scopes: z.array(z.string()) }).optional(),
      serviceAccount: z.object({ jwtBearer: z.object({ scopes: z.array(z.string()) }) }).optional(),
    })) }),
    operations: z.array(z.object({
      name: z.string(), kind: z.enum(['query', 'mutation']), idempotency: z.enum(['none', 'required']),
      branches: z.array(z.object({ id: z.string(), description: z.string(), optional: z.boolean().optional() })),
      execution: z.object({ executeMethodTimeout: z.string(), durability: z.string(), retry: z.object({
        initialInterval: z.string(), backoffCoefficient: z.number(), maximumInterval: z.string(),
        maximumAttempts: z.number(), totalDuration: z.string(),
      }).optional() }),
      mocks: z.array(z.object({
        name: z.string(), branch: z.string().optional(), default: z.boolean().optional(),
        output: z.unknown().optional(), failure: FailureSchema.optional(),
        retry: z.object({ failure: FailureSchema, after: z.string().optional() }).optional(),
      })),
    })),
  }),
});

const definitionByOperation = {
  getMessage: GetMessageDefinition,
  sendMessage: SendMessageDefinition,
  replyToMessage: ReplyToMessageDefinition,
};

const cases = [
  { operation: 'getMessage', name: 'customerQuestion', create: gmailmock.getMessageCustomerQuestion },
  { operation: 'getMessage', name: 'deletedMessage', create: gmailmock.getMessageDeletedMessage },
  { operation: 'getMessage', name: 'revokedGrant', create: gmailmock.getMessageRevokedGrant },
  { operation: 'getMessage', name: 'malformedPayload', create: gmailmock.getMessageMalformedPayload },
  { operation: 'getMessage', name: 'emptyMessageID', create: gmailmock.getMessageEmptyMessageID },
  { operation: 'getMessage', name: 'rateLimited', create: gmailmock.getMessageRateLimited },
  { operation: 'sendMessage', name: 'sentToCustomer', create: gmailmock.sendMessageSentToCustomer },
  { operation: 'sendMessage', name: 'invalidRecipient', create: gmailmock.sendMessageInvalidRecipient },
  { operation: 'sendMessage', name: 'connectionLost', create: gmailmock.sendMessageConnectionLost },
  { operation: 'sendMessage', name: 'noRecipients', create: gmailmock.sendMessageNoRecipients },
  { operation: 'sendMessage', name: 'unavailable', create: gmailmock.sendMessageUnavailable },
  { operation: 'replyToMessage', name: 'repliedInThread', create: gmailmock.replyToMessageRepliedInThread },
  { operation: 'replyToMessage', name: 'sourceMissing', create: gmailmock.replyToMessageSourceMissing },
  { operation: 'replyToMessage', name: 'malformedSource', create: gmailmock.replyToMessageMalformedSource },
  { operation: 'replyToMessage', name: 'connectionLost', create: gmailmock.replyToMessageConnectionLost },
  { operation: 'replyToMessage', name: 'emptyBody', create: gmailmock.replyToMessageEmptyBody },
];

function durationMs(value: string): number {
  const match = /^(\d+)(ms|s|m)$/.exec(value);
  if (!match) throw new TypeError('manifest duration is unsupported by Gmail TypeScript drift check');
  return Number(match[1]) * { ms: 1, s: 1000, m: 60_000 }[match[2] as 'ms' | 's' | 'm'];
}

describe('connector.yaml remains the source of Gmail TypeScript definitions and mocks', () => {
  it('matches every operation identity, branch, and Execute default', async () => {
    const manifest = ManifestSchema.parse(parse(await readFile(new URL('../../connector.yaml', import.meta.url), 'utf8')) as unknown);
    expect(manifest.spec.languages.typescript.version).toBe('v0.1.0');
    expect(manifest.spec.operations.map(operation => operation.name).sort()).toEqual(Object.keys(definitionByOperation).sort());
    for (const operation of manifest.spec.operations) {
      expect(operation.kind).toBe(operation.name === 'getMessage' ? 'query' : 'mutation');
      expect(operation.idempotency).toBe(operation.name === 'getMessage' ? 'none' : 'required');
      const definition = definitionByOperation[operation.name as keyof typeof definitionByOperation];
      expect(definition.operation).toEqual({ connectorId: 'gmail', operationId: operation.name });
      expect(definition.branches).toEqual(operation.branches);
      expect(definition.stepDefaults.executeMethodTimeoutMs).toBe(durationMs(operation.execution.executeMethodTimeout));
      expect(definition.stepDefaults.executeDurability).toBe(operation.execution.durability);
      const retry = operation.execution.retry;
      if (retry) expect(definition.stepDefaults.executeRetry).toEqual({
        initialIntervalMs: durationMs(retry.initialInterval), backoffCoefficient: retry.backoffCoefficient,
        maximumIntervalMs: durationMs(retry.maximumInterval), maximumAttempts: retry.maximumAttempts,
        totalDurationMs: durationMs(retry.totalDuration),
      });
      else expect(definition.stepDefaults.executeRetry).toBeUndefined();
    }
  });

  it('retains the OAuth and Workspace authorization fields used by the credential codec', async () => {
    const manifest = ManifestSchema.parse(parse(await readFile(new URL('../../connector.yaml', import.meta.url), 'utf8')) as unknown);
    expect(manifest.spec.auth.refreshable).toBe(true);
    expect(manifest.spec.auth.defaultMethod).toBe(GoogleOAuthAuthMethodID);
    expect(manifest.spec.auth.methods.map(method => method.id)).toEqual([GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID]);
    const oauth = manifest.spec.auth.methods[0];
    expect(oauth?.type).toBe('oauth2');
    expect(oauth?.fields.map(field => field.name)).toEqual(['oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'primary_email']);
    expect(oauth?.oauth2).toEqual({
      tokenEndpoint: 'https://oauth2.googleapis.com/token',
      scopes: ['openid', 'https://www.googleapis.com/auth/userinfo.email', 'https://www.googleapis.com/auth/gmail.readonly', 'https://www.googleapis.com/auth/gmail.send'],
    });
    const workspace = manifest.spec.auth.methods[1];
    expect(workspace?.type).toBe('serviceAccount');
    expect(workspace?.fields.map(field => field.name)).toEqual(['service_account_key', 'delegated_user', 'access_token', 'primary_email']);
    expect(workspace?.serviceAccount?.jwtBearer.scopes).toEqual(['https://www.googleapis.com/auth/gmail.readonly', 'https://www.googleapis.com/auth/gmail.send']);
  });

  it('matches every named manifest case, failure, retry, and default', async () => {
    const manifest = ManifestSchema.parse(parse(await readFile(new URL('../../connector.yaml', import.meta.url), 'utf8')) as unknown);
    const manifestNames = manifest.spec.operations.flatMap(operation => operation.mocks.map(mock => `${operation.name}.${mock.name}`)).sort();
    expect(cases.map(entry => `${entry.operation}.${entry.name}`).sort()).toEqual(manifestNames);
    for (const entry of cases) {
      const mock = manifest.spec.operations.find(operation => operation.name === entry.operation)?.mocks.find(candidate => candidate.name === entry.name);
      if (!mock) throw new TypeError('Gmail manifest mock case is missing');
      const actual = entry.create();
      if (mock.retry) {
        expect(actual).toMatchObject({ kind: 'retry', failure: mock.retry.failure, retryAfterMs: mock.retry.after ? durationMs(mock.retry.after) : 0 });
      } else {
        expect(actual).toMatchObject({ kind: 'branch', branch: mock.branch });
        if (mock.failure) expect(actual).toMatchObject({ failure: mock.failure });
        if (mock.output !== undefined && actual.kind === 'branch') expect(actual.value).toEqual(mock.output);
      }
    }
    expect(manifest.spec.operations.filter(operation => operation.name === 'getMessage')[0]?.mocks.find(mock => mock.default)?.name).toBe('customerQuestion');
    expect(manifest.spec.operations.filter(operation => operation.name === 'sendMessage')[0]?.mocks.find(mock => mock.default)?.name).toBe('sentToCustomer');
    expect(manifest.spec.operations.filter(operation => operation.name === 'replyToMessage')[0]?.mocks.find(mock => mock.default)?.name).toBe('repliedInThread');
  });
});
