// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { YuandianClient } from '../src/client.js';
import { GetCompanyProfileInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/yuandian/scenarios.json', import.meta.url));
const connection = { provider: 'yuandian', name: 'yuandian' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYuandian));

async function invokeYuandian(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getCompanyProfile') throw new Error(`unknown Yuandian operation ${scenario.operation}`);
  const client = new YuandianClient(scenario.name === 'getCompanyProfile-oversized' ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, { apiKey: new SecretString(scenario.name === 'getCompanyProfile-defect' ? '' : 'test-secret') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'yuandian-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'yuandian', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getCompanyProfile().invoke(call, GetCompanyProfileInputSchema.parse(scenario.input));
}
