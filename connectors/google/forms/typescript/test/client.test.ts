// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { GoogleFormsClient } from '../src/client.js';
import { loadScenarios, scenarioCall } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const connection = { provider: 'google', name: 'forms-test' };
const scenario = loadScenarios(new URL('../../../../../conformance/connectors/google/forms/scenarios.json', import.meta.url))[0]!;
function call(): Call {
  return scenarioCall(scenario, connection, 'google-forms', { flowId: 'forms-unit', cancellationSignal: new AbortController().signal },
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
}
function credentialSource() { return new StaticCredentialProvider([[connection, { accessToken: new SecretString('one-use-token') }]]); }

it('rejects invalid response limits and page sizes before network access', () => {
  expect(() => new GoogleFormsClient({ maxResponseBytes: -1 }, credentialSource())).toThrow();
  expect(() => new GoogleFormsClient({ responsePageSize: 1001 }, credentialSource())).toThrow();
});

it('classifies a large success body as invalidResponse but a large 429 by status', async () => {
  const success = new GoogleFormsClient({ endpoint: 'http://127.0.0.1:41801', maxResponseBytes: 10 }, credentialSource(), {
    send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"formId":"form_123"}') }),
  });
  expect((await success.getForm().invoke(call(), { formId: 'form_123' })).kind).toBe('branch');
  const rateLimited = new GoogleFormsClient({ endpoint: 'http://127.0.0.1:41801', maxResponseBytes: 10 }, credentialSource(), {
    send: async () => ({ status: 429, headers: {}, body: Buffer.alloc(1000, 'x') }),
  });
  expect((await rateLimited.getForm().invoke(call(), { formId: 'form_123' })).kind).toBe('retry');
});
