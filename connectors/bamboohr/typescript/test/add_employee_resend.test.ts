// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { BambooHRClient } from '../src/client.js';
import { AddEmployeeDefinition } from '../src/generated/definitions.js';
import type { AddEmployeeOutput } from '../src/generated/models.js';

const callId = '6c9e932e-fe09-5f01-8889-39650948771e';

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'add-employee-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

interface BambooHRAnswer { status: number; body: string }

function newClient(answer: (request: ProviderRequest) => BambooHRAnswer) {
  const requests: ProviderRequest[] = [];
  const client = new BambooHRClient({ companyDomain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inert-key') }) }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answer(request);
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

async function add(client: BambooHRClient, context: AttemptContext): Promise<MutationResult<AddEmployeeOutput> | RetryError> {
  const call = { id: callId, connection: { provider: 'bamboohr', name: 'bamboohr' }, operation: AddEmployeeDefinition.operation, context } as unknown as Call;
  try { return await runMutation(client.addEmployee(), call, { firstName: 'Sam', lastName: 'Park' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('BambooHR addEmployee bounded resend', () => {
  it.each([500, 408])('resends an unknown HTTP %i until the send cap, then selects uncertain', async status => {
    const { client, requests } = newClient(() => ({ status, body: '{}' }));
    const context = new AttemptContext();
    for (let attempt = 1; attempt < AddEmployeeDefinition.maxSends!; attempt++) {
      context.attempt = attempt;
      expect(await add(client, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: attempt });
    }
    context.attempt = AddEmployeeDefinition.maxSends!;
    expect(await add(client, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await add(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(AddEmployeeDefinition.maxSends!);
  });

  it.each([400, 409])('records canResend before selecting providerRejected for a definite first-send HTTP %i', async status => {
    const { client, requests } = newClient(() => ({ status, body: '{}' }));
    const context = new AttemptContext();
    expect(await add(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it.each([400, 409])('selects uncertain for HTTP %i after a possible send', async status => {
    let index = 0;
    const { client, requests } = newClient(() => ({ status: index++ === 0 ? 502 : status, body: '{}' }));
    const context = new AttemptContext();
    expect(await add(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await add(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('never resends an accepted POST whose body is unusable', async () => {
    const { client, requests } = newClient(() => ({ status: 201, body: '{"firstName":"Sam"}' }));
    expect(await add(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('reads back a saved employee ID instead of resending', async () => {
    const { client, requests } = newClient(() => ({ status: 200, body: '{"id":"142"}' }));
    const context = new AttemptContext();
    context.attempt = 2;
    context.marker = { callId, attempt: 1, state: 'claimed', providerId: '142', sendCount: 1 };
    expect(await add(client, context)).toMatchObject({ branch: 'created', value: { employeeId: '142' } });
    expect(requests.map(request => request.method)).toEqual(['GET']);
  });

  it.each([
    [{ location: 'https://acme.bamboohr.com/api/v1/employees/142' }, 'created'],
    [{}, 'uncertain'],
  ] as const)('never resends a 2xx whose body breaks after the headers (%o)', async (headers, branch) => {
    const requests: ProviderRequest[] = [];
    const client = new BambooHRClient({ companyDomain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inert-key') }) }, {
      send: async (request: ProviderRequest) => { requests.push(request); throw new ProviderTransportError(false, 201, { ...headers }); },
    });
    expect(await add(client, new AttemptContext())).toMatchObject({ branch });
    expect(requests).toHaveLength(1);
  });

  it.each([429, 503])('records canResend for an HTTP %i whose body breaks, so it is not counted as a send', async status => {
    const requests: ProviderRequest[] = [];
    const client = new BambooHRClient({ companyDomain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inert-key') }) }, {
      send: async (request: ProviderRequest) => { requests.push(request); throw new ProviderTransportError(false, status, {}); },
    });
    const context = new AttemptContext();
    expect(await add(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('reads the Location ID of an accepted POST whose body echoes the credential', async () => {
    const requests: ProviderRequest[] = [];
    const client = new BambooHRClient({ companyDomain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inert-key') }) }, {
      send: async (request: ProviderRequest) => {
        requests.push(request);
        return { status: 201, headers: { location: 'https://acme.bamboohr.com/api/v1/employees/142' }, body: Buffer.from('{"echo":"inert-key"}') };
      },
    });
    const result = await add(client, new AttemptContext());
    expect(result).toMatchObject({ branch: 'created', value: { employeeId: '142' } });
    expect(JSON.stringify(result)).not.toContain('inert-key');
    expect(requests).toHaveLength(1);
  });

  it('counts a send before a 429 and selects uncertain for a later refusal', async () => {
    const statuses = [500, 429, 400];
    const { client, requests } = newClient(() => ({ status: statuses[requests.length - 1]!, body: '{}' }));
    const context = new AttemptContext();
    expect(await add(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await add(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 3;
    expect(await add(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'POST', 'POST']);
  });

  it('selects uncertain when a saved employee cannot be read back with terminal credentials', async () => {
    const requests: ProviderRequest[] = [];
    const client = new BambooHRClient({ companyDomain: 'acme' }, { resolve: async () => { throw new ReauthorizationRequiredError(); } }, {
      send: async (request: ProviderRequest) => { requests.push(request); return { status: 200, headers: {}, body: Buffer.from('{}') }; },
    });
    const context = new AttemptContext();
    context.attempt = 2;
    context.marker = { callId, attempt: 1, state: 'claimed', providerId: '142', sendCount: 1 };
    expect(await add(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(0);
  });

  it('returns Retry without sending for an untrusted checkpoint', async () => {
    const { client, requests } = newClient(() => ({ status: 201, body: '{"id":"142"}' }));
    const context = new AttemptContext();
    context.attempt = 2;
    context.marker = { bamboohrDispatchedCallId: 'earlier' };
    expect(await add(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });
});
