// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { BoxClient } from '../src/client.js';
import { CredentialMaterialError, decodeCredentials } from '../src/generated/models.js';
import type { Credentials } from '../src/models.js';
import { HeartbeatContext, loadScenarios, scenarioCall } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenario = loadScenarios(new URL('../../../../conformance/connectors/box/scenarios.json', import.meta.url))[0]!;
const credentials: Credentials = {
  oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'),
  accessToken: new SecretString('test-token'), refreshToken: new SecretString('refresh-token'),
};
const driver: CredentialRefreshDriver<Credentials> = {
  refreshRequired: () => false,
  refresh: async () => { throw new Error('unexpected refresh'); },
};

function newClient(send: (request: ProviderRequest) => Promise<ProviderResponse>): BoxClient {
  return new BoxClient({ baseUrl: 'https://api.box.com' }, { resolve: async () => credentials }, {
    refreshDriver: driver, send,
  });
}

function call(): Call {
  return scenarioCall(scenario, { provider: 'box', name: 'box' }, 'box', new HeartbeatContext(undefined)) as Call;
}

describe('Box signed download redirect', () => {
  it('follows Box content URL without forwarding the OAuth token', async () => {
    const requests: ProviderRequest[] = [];
    const client = newClient(async request => {
      requests.push(request);
      return requests.length === 1
        ? { status: 302, headers: { location: 'https://dl.boxcloud.com/d/1/signed/download' }, body: new Uint8Array() }
        : { status: 200, headers: {}, body: Buffer.from('Hello') };
    });
    const result = await client.downloadFile().invoke(call(), { fileId: '12345' });
    expect(result.kind).toBe('branch');
    expect(requests).toHaveLength(2);
    expect(requests[0]?.headers?.Authorization).toBe('Bearer test-token');
    expect(requests[1]?.headers?.Authorization).toBeUndefined();
    expect(requests[1]?.url).toBe('https://dl.boxcloud.com/d/1/signed/download');
  });

  it('rejects a redirect outside Box before sending another request', async () => {
    const requests: ProviderRequest[] = [];
    const client = newClient(async request => {
      requests.push(request);
      return { status: 302, headers: { location: 'https://dl.boxcloud.com.evil.example/download' }, body: new Uint8Array() };
    });
    const result = await client.downloadFile().invoke(call(), { fileId: '12345' });
    expect(result.kind).toBe('branch');
    expect(requests).toHaveLength(1);
    if (result.kind === 'branch') expect(result.branch).toBe('invalidResponse');
  });

  it.each([
    ['broker unavailable', new CredentialBrokerUnavailableError(), 'retry'],
    ['static not configured', new Error('connection is not configured'), 'retry'],
    ['unlisted error', new RangeError('credential store hiccup'), 'retry'],
    ['reauthorization', new ReauthorizationRequiredError(), 'branch'],
    ['object not found', new ObjectNotFoundError(), 'branch'],
    ['connection not selected', new ConnectionNotSelectedError(), 'branch'],
    ['broker rejected', new CredentialBrokerRejectedError(), 'branch'],
    ['malformed stored credential', new CredentialMaterialError('credential access_token is required'), 'branch'],
    ['SDK-wrapped malformed stored credential', sdkDecodingFailure(), 'branch'],
  ])('classifies a %s credential failure before any request', async (_name, error, kind) => {
    const requests: ProviderRequest[] = [];
    const client = new BoxClient({ baseUrl: 'https://api.box.com' }, { resolve: async () => { throw error; } }, {
      refreshDriver: driver, send: async request => { requests.push(request); throw new Error('no request may be sent'); },
    });
    const result = await client.downloadFile().invoke(call(), { fileId: '12345' });
    expect(result.kind).toBe(kind);
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
    expect(requests).toHaveLength(0);
  });
});

/** Reproduce the SDK refresh provider, which replaces the decoder error with this TypeError. */
function sdkDecodingFailure(): TypeError {
  try { decodeCredentials({ access_token: 7 }); } catch { return new TypeError('project credential decoding failed'); }
  throw new Error('malformed material decoded');
}

const oldMetadata = `{"id":"12345","type":"file","name":"example.txt","sha1":"${'0'.repeat(40)}","size":5}`;

describe('Box uploadFile classification', () => {
  it('retries a terminal credential failure on a later attempt, which may follow an applied upload', async () => {
    const client = new BoxClient({ baseUrl: 'https://api.box.com' }, { resolve: async () => { throw new ReauthorizationRequiredError(); } }, {
      refreshDriver: driver, send: async () => { throw new Error('no request may be sent'); },
    });
    const later = { ...call(), context: { ...call().context, attempt: 2 } } as Call;
    expect((await client.uploadFile().invoke(later, { fileId: '12345', contentBase64: 'SGVsbG8=' })).kind).toBe('retry');
  });

  it.each([
    ['malformed metadata', 200, '{"id":"12345"}', 0, '', 'retry', 0, 1],
    ['an unlisted metadata status', 409, '{"code":"conflict"}', 0, '', 'retry', 0, 1],
    ['a metadata refusal describing the file', 403, oldMetadata, 0, '', 'retry', 0, 1],
    ['a definite metadata refusal', 403, '{"code":"forbidden"}', 0, '', 'branch', 0, 1],
    ['an unlisted upload status', 200, oldMetadata, 409, '{"type":"error","code":"conflict"}', 'retry', 1, 2],
    ['an upload refusal carrying the file', 200, oldMetadata, 400, `{"entries":[${oldMetadata}]}`, 'retry', 1, 2],
    ['a definite upload refusal', 200, oldMetadata, 403, '{"type":"error","code":"access_denied"}', 'branch', 1, 1],
  ] as const)('selects a branch only for a definite answer: %s', async (_name, metadataStatus, metadataBody, uploadStatus, uploadBody, kind, posts, gets) => {
    const counts = { GET: 0, POST: 0 };
    const client = newClient(async request => {
      const method = request.method as 'GET' | 'POST';
      counts[method] += 1;
      if (method === 'POST') return { status: uploadStatus, headers: {}, body: Buffer.from(uploadBody) };
      return { status: counts.GET === 1 ? metadataStatus : 200, headers: {}, body: Buffer.from(metadataBody) };
    });
    const result = await client.uploadFile().invoke(call(), { fileId: '12345', contentBase64: 'SGVsbG8=' });
    expect(result.kind).toBe(kind);
    expect(counts).toEqual({ GET: gets, POST: posts });
  });
});
