// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OppoClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/oppo/scenarios.json', import.meta.url));
const connection = { provider: 'oppo', name: 'oppo' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOppo, ''));

async function invokeOppo(scenario: Scenario, recorder: ExchangeRecorder) {
  const appKey = new SecretString(scenario.name.endsWith('defect') ? '' : 'test-app-key');
  const credentials = new StaticCredentialProvider([[connection, { appKey, masterSecret: new SecretString('test-master-secret') }]]);
  const client = new OppoClient({}, credentials, {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date(1731510000000),
  });
  const call: Call = {
    context: { flowId: 'oppo-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'oppo', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getPushQuota') return client.getPushQuota().invoke(call, scenario.input);
  throw new Error(`unknown OPPO operation ${scenario.operation}`);
}
