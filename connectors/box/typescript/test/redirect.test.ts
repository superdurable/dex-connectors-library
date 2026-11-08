// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { BoxClient } from '../src/client.js';
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
});
