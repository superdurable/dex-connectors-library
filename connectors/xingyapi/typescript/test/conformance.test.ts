// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { XingyAPIClient } from '../src/client.js';
import { GetInstanceOnlineStatusInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/xingyapi/scenarios.json', import.meta.url));
const connection = { provider: 'xingyapi', name: 'xingyapi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeXingyAPI));

async function invokeXingyAPI(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getInstanceOnlineStatus') throw new Error(`unknown XingyAPI operation ${scenario.operation}`);
  const client = new XingyAPIClient(scenario.name === 'getInstanceOnlineStatus-oversized' ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, { apiKey: new SecretString(scenario.name === 'getInstanceOnlineStatus-defect' ? '' : 'test-secret') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'xingyapi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'xingyapi', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getInstanceOnlineStatus().invoke(call, GetInstanceOnlineStatusInputSchema.parse(scenario.input));
}
