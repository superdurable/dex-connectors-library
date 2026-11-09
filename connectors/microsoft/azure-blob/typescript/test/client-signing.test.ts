// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { MicrosoftAzureBlobClient } from '../src/client.js';

const connection = { provider: 'microsoft-azure-blob', name: 'microsoft-azure-blob' };
const encodedPath = '/docs/reports/q1%20r%C3%A9sum%C3%A9.txt';

it('signs the same percent-encoded path that the request sends', async () => {
  const requests: ProviderRequest[] = [];
  const credentials: Array<[typeof connection, { accountKey: SecretString }]> = [[connection, { accountKey: new SecretString('c2VjcmV0LXRlc3Qta2V5') }]];
  const client = new MicrosoftAzureBlobClient({ endpoint: 'https://exampleacct.blob.core.windows.net', accountName: 'exampleacct' }, new StaticCredentialProvider(credentials), {
    send: async request => {
      requests.push(request);
      return { status: 200, headers: { 'content-type': 'text/plain' }, body: new TextEncoder().encode('report') };
    },
    now: () => new Date('2026-10-30T00:00:00Z'),
  });
  const call: Call = {
    context: { flowId: 'azureblob-signing-test', cancellationSignal: new AbortController().signal } as Call['context'],
    id: 'azure-blob-signing-test', connection, operation: { connectorId: 'microsoft-azure-blob', operationId: 'getBlob' },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  const attempt = await client.getBlob().invoke(call, { container: 'docs', blobName: 'reports/q1 résumé.txt' });
  const signedValue = `GET${'\n'.repeat(12)}x-ms-date:Fri, 30 Oct 2026 00:00:00 GMT\nx-ms-version:2023-11-03\n/exampleacct${encodedPath}`;
  const signature = createHmac('sha256', Buffer.from('secret-test-key')).update(signedValue).digest('base64');
  expect(attempt.kind === 'branch' ? attempt.branch : attempt.kind).toBe('downloaded');
  expect(requests).toHaveLength(1);
  expect(new URL(requests[0]!.url).pathname).toBe(encodedPath);
  expect(requests[0]!.headers).toMatchObject({ Authorization: `SharedKey exampleacct:${signature}` });
});
