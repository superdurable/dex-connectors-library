// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
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
