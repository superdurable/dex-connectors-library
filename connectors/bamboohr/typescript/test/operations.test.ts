// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { BambooHRClient } from '../src/client.js';
import { buildTimeOffRequestFilter } from '../src/operations.js';
import { CredentialMaterialError, type Credentials } from '../src/generated/models.js';

const credentials = { resolve: async () => ({ apiKey: new SecretString('0123456789abcdef0123456789abcdef01234567') }) };
const call = {
  id: '5af8fafa-06ff-5b2d-bb13-04693809fabf',
  context: {
    attempt: 1, cancellationSignal: new AbortController().signal,
    hasLastHeartbeatValue: () => false,
    recordHeartbeat: async () => {},
  },
} as unknown as Call;

function clientWithCredentialError(error: Error, send: () => Promise<never> = async () => { throw new Error('provider request must not be sent'); }): BambooHRClient {
  const provider: CredentialProvider<Credentials> = { resolve: async () => { throw error; } };
  return new BambooHRClient({ companyDomain: 'acme' }, provider, { send });
}

function invokeOperation(client: BambooHRClient, operation: string) {
  switch (operation) {
    case 'getEmployee': return client.getEmployee().invoke(call, { employeeId: '123', fields: ['firstName'] });
    case 'findEmployeeByEmail': return client.findEmployeeByEmail().invoke(call, { email: 'ava@example.com' });
    case 'listEmployeeChanges': return client.listEmployeeChanges().invoke(call, { since: '2026-01-01T00:00:00Z' });
    case 'listTimeOffRequests': return client.listTimeOffRequests().invoke(call, { startDate: '2026-01-01', endDate: '2026-01-31' });
    case 'updateEmployee': return client.updateEmployee().invoke(call, { employeeId: '123', fields: { firstName: 'Ava' } });
    case 'addEmployee': return client.addEmployee().invoke(call, { firstName: 'Ava', lastName: 'Nguyen' });
    default: throw new Error(`unknown operation ${operation}`);
  }
}

describe('BambooHR client', () => {
  it('rejects a company domain that could redirect credentials', () => {
    expect(() => new BambooHRClient({ companyDomain: 'other.example.com' }, credentials)).toThrow();
    expect(() => new BambooHRClient({ companyDomain: 'acme/path' }, credentials)).toThrow();
  });

  it('builds an overlap filter with lowercase provider statuses', () => {
    expect(buildTimeOffRequestFilter({ startDate: '2026-10-13', endDate: '2026-10-26', employeeId: '123', statuses: ['REQUESTED', 'APPROVED'] }))
      .toBe("startDate le '2026-10-26' and endDate ge '2026-10-13' and employeeId eq 123 and status in ('requested', 'approved')");
  });

  it('updateEmployee retries a transient credential failure and selects defect for a terminal one', async () => {
    for (const error of [new Error('temporary credential store failure'), new Error('connection is not configured'), new CredentialBrokerUnavailableError()]) {
      const transient = await invokeOperation(clientWithCredentialError(error), 'updateEmployee');
      expect(transient.kind).toBe('retry');
    }
    for (const error of [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError()]) {
      const terminal = await invokeOperation(clientWithCredentialError(error), 'updateEmployee');
      expect(terminal).toMatchObject({ kind: 'branch', branch: 'defect' });
    }
  });

  it('updateEmployee retries every failed read-back after an accepted write', async () => {
    for (const [readBack, expected] of [[{ status: 200, body: '{"id":"999"}' }, 'retry'], [{ status: 404, body: '{}' }, 'retry']] as const) {
      const answers = [{ status: 200, body: '{"id":"123","firstName":"Sam"}' }, { status: 200, body: '' }, readBack];
      const client = new BambooHRClient({ companyDomain: 'acme' }, credentials, {
        send: async () => { const answer = answers.shift()!; return { status: answer.status, headers: {}, body: Buffer.from(answer.body) }; },
      });
      const attempt = await invokeOperation(client, 'updateEmployee');
      expect(attempt.kind).toBe(expected);
    }
  });

  it.each(['getEmployee', 'findEmployeeByEmail', 'listEmployeeChanges', 'listTimeOffRequests'])('%s selects defect for a terminal credential failure and retries a transient one', async operation => {
    const terminalErrors = [new ReauthorizationRequiredError(), new CredentialBrokerRejectedError(), new CredentialMaterialError('credential api_key is required'), new TypeError('project credential decoding failed')];
    for (const error of terminalErrors) {
      const attempt = await invokeOperation(clientWithCredentialError(error), operation);
      expect(attempt.kind).toBe('branch');
      if (attempt.kind === 'branch') {
        expect(attempt.branch).toBe('defect');
        expect(attempt.failure?.kind).toBe('AUTHENTICATION');
      }
    }
    for (const error of [new Error('temporary credential store failure'), new CredentialBrokerUnavailableError(), new TypeError('unlisted')]) {
      const attempt = await invokeOperation(clientWithCredentialError(error), operation);
      expect(attempt.kind).toBe('retry');
      if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('AVAILABILITY');
    }
  });

  it.each([new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError()])('addEmployee treats terminal credential errors as defects after recording not-sent', async error => {
    const checkpoints: unknown[] = [];
    const terminalCall = { ...call, context: { ...call.context, recordHeartbeat: async (value: unknown) => { checkpoints.push(value); } } } as Call;
    const attempt = await clientWithCredentialError(error).addEmployee().invoke(terminalCall, { firstName: 'Ava', lastName: 'Nguyen' });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') {
      expect(attempt.branch).toBe('defect');
      expect(attempt.failure?.kind).toBe('AUTHENTICATION');
    }
    expect(checkpoints).toEqual([{ callId: call.id, attempt: 1, state: 'notSent' }]);
  });

  it('addEmployee treats an unusable resolved key as a terminal authentication defect', async () => {
    const client = new BambooHRClient({ companyDomain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('bad:key') }) }, {
      send: async () => { throw new Error('provider request must not be sent'); },
    });
    const attempt = await invokeOperation(client, 'addEmployee');
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') {
      expect(attempt.branch).toBe('defect');
      expect(attempt.failure?.kind).toBe('AUTHENTICATION');
    }
  });

  it('addEmployee retries a transient credential failure after recording not-sent', async () => {
    const checkpoints: unknown[] = [];
    const retryCall = { ...call, context: { ...call.context, recordHeartbeat: async (value: unknown) => { checkpoints.push(value); } } } as Call;
    const client = clientWithCredentialError(new Error('temporary credential store failure'));
    const attempt = await client.addEmployee().invoke(retryCall, { firstName: 'Ava', lastName: 'Nguyen' });
    expect(attempt.kind).toBe('retry');
    if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('AVAILABILITY');
    expect(checkpoints).toEqual([{ callId: call.id, attempt: 1, state: 'notSent' }]);
  });

  it('addEmployee retries a transient credential failure during read-back without sending another write', async () => {
    const readBackCall = { ...call, context: { ...call.context, attempt: 2, hasLastHeartbeatValue: () => true,
      getLastHeartbeatValue: () => ({ callId: call.id, attempt: 1, state: 'claimed', providerId: '123' }) } } as Call;
    const attempt = await clientWithCredentialError(new Error('temporary credential store failure')).addEmployee().invoke(readBackCall, { firstName: 'Ava', lastName: 'Nguyen' });
    expect(attempt.kind).toBe('retry');
    if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('AUTHENTICATION');
  });

  it('selects provider address policy from the parsed hostname', async () => {
    const policies: string[] = [];
    for (const endpoint of ['https://localhost.example.com/api/v1', 'http://localhost/api/v1']) {
      const client = new BambooHRClient({ companyDomain: 'acme' }, credentials, {
        apiBaseURL: endpoint,
        send: async request => { policies.push(request.addressPolicy); return { status: 404, headers: {}, body: Buffer.from('{}') }; },
      });
      await client.getEmployee().invoke(call, { employeeId: '123', fields: ['firstName'] });
    }
    expect(policies).toEqual(['publicHTTPS', 'privateNetworkHTTP']);
  });
});
