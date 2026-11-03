// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { BambooHRClient } from '../src/client.js';
import { buildTimeOffRequestFilter } from '../src/operations.js';
import type { Credentials } from '../src/generated/models.js';

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

  it.each(['getEmployee', 'findEmployeeByEmail', 'listEmployeeChanges', 'listTimeOffRequests', 'updateEmployee'])('%s returns the Go authentication defect for terminal and transient credential failures', async operation => {
    for (const error of [new ReauthorizationRequiredError(), new Error('temporary credential store failure')]) {
      const attempt = await invokeOperation(clientWithCredentialError(error), operation);
      expect(attempt.kind).toBe('branch');
      if (attempt.kind === 'branch') {
        expect(attempt.branch).toBe('defect');
        expect(attempt.failure?.kind).toBe('AUTHENTICATION');
      }
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

  it('addEmployee retries credential failure during read-back without sending another write', async () => {
    const readBackCall = { ...call, context: { ...call.context, attempt: 2, hasLastHeartbeatValue: () => true,
      getLastHeartbeatValue: () => ({ callId: call.id, attempt: 1, state: 'claimed', providerId: '123' }) } } as Call;
    const attempt = await clientWithCredentialError(new ReauthorizationRequiredError()).addEmployee().invoke(readBackCall, { firstName: 'Ava', lastName: 'Nguyen' });
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
