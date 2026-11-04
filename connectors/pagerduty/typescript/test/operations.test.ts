// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { PagerDutyClient } from '../src/client.js';
import { BodyTooLargeError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

const call = {
  id: 'pagerduty-test', connection: { provider: 'pagerduty', name: 'pagerduty' },
  context: { flowId: 'pagerduty-test', cancellationSignal: new AbortController().signal } as Call['context'],
  operation: { connectorId: 'pagerduty', operationId: 'getIncident' },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as Call;

function client(send: (request: ProviderRequest) => Promise<ProviderResponse>, token = 'test-secret-token') {
  return new PagerDutyClient({ baseUrl: 'http://127.0.0.1:26042' }, { resolve: async () => ({ apiKey: new SecretString(token) }) }, { send });
}

describe('PagerDuty operation classification', () => {
  it('escapes a path segment like Go url.PathEscape', async () => {
    let sent = '';
    const operation = client(async request => {
      sent = request.url;
      return { status: 200, headers: {}, body: Buffer.from('{"incident":{"id":"A/B, C","title":"T","status":"triggered"}}') };
    }).getIncident();
    const result = await operation.invoke(call, { id: 'A/B, C' });
    expect(result.kind).toBe('branch');
    expect(sent).toContain('/incidents/A%2FB%2C%20C');
  });
  it('keeps credential failures terminal and transport failures retryable', async () => {
    const sender = async (): Promise<ProviderResponse> => { throw new Error('connection refused'); };
    const invalid = await client(sender, '\n').listIncidents().invoke(call, { limit: 25, offset: 0 });
    expect(invalid.kind).toBe('branch');
    const refused = await client(sender).listIncidents().invoke(call, { limit: 25, offset: 0 });
    expect(refused.kind).toBe('retry');
  });
  it('classifies oversized error bodies by HTTP status and preserves Retry-After', async () => {
    for (const [status, expectedDisposition, expectedKind] of [
      [429, 'retry', 'RATE_LIMIT'], [503, 'retry', 'AVAILABILITY'],
      [404, 'notFound', 'NOT_FOUND'], [400, 'providerRejected', 'PROVIDER_REJECTION'],
      [200, 'invalidResponse', 'RESPONSE_TOO_LARGE'],
    ] as const) {
      const pagerDuty = client(async () => { throw new BodyTooLargeError(status, { 'retry-after': '2' }); });
      const result = await pagerDuty.exchange(call, 'getIncident', '/incidents/example', true);
      expect(result).toHaveProperty('outcome.disposition', expectedDisposition);
      expect(result).toHaveProperty('outcome.failure.kind', expectedKind);
      if (status === 429 || status === 503) expect(result).toHaveProperty('outcome.retryAfterMs', 2_000);
    }
  });
});
