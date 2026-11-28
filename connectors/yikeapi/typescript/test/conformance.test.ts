// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { YikeAPIClient } from '../src/client.js';
import { LookupIPLocationInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/yikeapi/scenarios.json', import.meta.url));
const connection = { provider: 'yikeapi', name: 'yikeapi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYikeAPI));

async function invokeYikeAPI(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'lookupIPLocation') throw new Error(`unknown YikeAPI operation ${scenario.operation}`);
  const client = new YikeAPIClient(scenario.name === 'lookupIPLocation-oversized' ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, { appID: new SecretString(scenario.name === 'lookupIPLocation-defect' ? '' : 'private-test-id'), appSecret: new SecretString('private-test-key') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  return client.lookupIPLocation().invoke(queryCall(scenario.callId), LookupIPLocationInputSchema.parse(scenario.input));
}

function queryCall(callID: Call['id']): Call {
  return {
    context: { flowId: 'yikeapi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: callID, connection, operation: { connectorId: 'yikeapi', operationId: 'lookupIPLocation' },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
}

it.each([
  { idLength: 1024, secretLength: 1024, expectedBranch: 'found', sends: 1 },
  { idLength: 1025, secretLength: 1024, expectedBranch: 'defect', sends: 0 },
  { idLength: 1024, secretLength: 1025, expectedBranch: 'defect', sends: 0 },
])('enforces both credential limits at $idLength/$secretLength', async ({ idLength, secretLength, expectedBranch, sends }) => {
  const appID = '+'.repeat(idLength);
  const appSecret = '&'.repeat(secretLength);
  const credentials = new StaticCredentialProvider([[connection, { appID: new SecretString(appID), appSecret: new SecretString(appSecret) }]]);
  const requests: URL[] = [];
  const client = new YikeAPIClient({}, credentials, {
    async send(request) {
      requests.push(new URL(request.url));
      expect(request.maxResponseBytes).toBe(65_536);
      const body = new TextEncoder().encode(JSON.stringify({ errcode: 0, ip: '8.8.8.8', country: 'US', city: 'Example' }));
      return { status: 200, headers: {}, body };
    },
  });
  const outcome = await client.lookupIPLocation().invoke(queryCall('62870a68-e30d-414b-b339-d9fe4bfb2c34'), { ipAddress: '8.8.8.8' });
  expect(outcome).toMatchObject({ kind: 'branch', branch: expectedBranch });
  expect(requests).toHaveLength(sends);
  if (sends) {
    expect(requests[0]?.searchParams.get('appid')).toBe(appID);
    expect(requests[0]?.searchParams.get('appsecret')).toBe(appSecret);
  } else {
    expect(outcome.failure?.kind).toBe('AUTHENTICATION');
  }
});
