// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { SalesforceClient } from '../src/client.js';

const connection = { provider: 'salesforce', name: 'salesforce-crm' };
const call = {
  id: 'c0a3b2d4-5e6f-5a7b-8c9d-0e1f2a3b4c5d', connection,
  operation: { connectorId: 'salesforce', operationId: 'updateRecord' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

it.each([409, 412, 302, 418])('retries a write answered with HTTP %i, which does not prove a refusal', async status => {
  const credentials = {
    authMethodId: 'salesforce-oauth', oauthClientId: 'inert-client-id',
    accessToken: new SecretString('salesforce-token-SENTINEL'), instanceUrl: 'http://127.0.0.1:12345',
  };
  const client = new SalesforceClient({}, new StaticCredentialProvider([[connection, credentials]]), {
    send: async () => ({ status, headers: {}, body: Buffer.from('[{"message":"no","errorCode":"UNKNOWN"}]') }),
  });
  const update = await client.updateRecord().invoke(call, { sObjectType: 'Opportunity', recordId: '006RM000001AbcdYAC', fields: { StageName: 'Closed Won' } });
  expect(update.kind).toBe('retry');
  const upsert = await client.upsertRecordByExternalId().invoke(call, { sObjectType: 'Contact', externalIdField: 'ERP_ID__c', externalIdValue: 'ERP-88213', fields: { LastName: 'Rao' } });
  expect(upsert.kind).toBe('retry');
});

it('retries an updateRecord answered with HTTP 300, which is defined only for external ID paths', async () => {
  const credentials = {
    authMethodId: 'salesforce-oauth', oauthClientId: 'inert-client-id',
    accessToken: new SecretString('salesforce-token-SENTINEL'), instanceUrl: 'http://127.0.0.1:12345',
  };
  const client = new SalesforceClient({}, new StaticCredentialProvider([[connection, credentials]]), {
    send: async () => ({ status: 300, headers: {}, body: Buffer.from('["/services/data/v62.0/sobjects/Opportunity/006RM000001AbcdYAC"]') }),
  });
  const update = await client.updateRecord().invoke(call, { sObjectType: 'Opportunity', recordId: '006RM000001AbcdYAC', fields: { StageName: 'Closed Won' } });
  expect(update.kind).toBe('retry');
});

it.each([
  ['broker unavailable', new CredentialBrokerUnavailableError(), 'retry', undefined],
  ['static not configured', new Error('connection is not configured'), 'retry', undefined],
  ['unlisted error', new RangeError('credential store hiccup'), 'retry', undefined],
  ['reauthorization', new ReauthorizationRequiredError(), 'branch', 'providerRejected'],
  ['object not found', new ObjectNotFoundError(), 'branch', 'defect'],
  ['connection not selected', new ConnectionNotSelectedError(), 'branch', 'defect'],
  ['broker rejected', new CredentialBrokerRejectedError(), 'branch', 'defect'],
])('classifies a %s credential failure on a write', async (_name, error, kind, branch) => {
  const failing = async () => { throw error; };
  const client = new SalesforceClient({}, { resolve: failing, resolveWithRefresh: failing }, { send: async () => { throw new Error('no request may be sent'); } });
  const update = await client.updateRecord().invoke(call, { sObjectType: 'Opportunity', recordId: '006RM000001AbcdYAC', fields: { StageName: 'Closed Won' } });
  expect(update.kind).toBe(kind);
  if (update.kind === 'branch') expect(update.branch).toBe(branch);
});
