// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZhihuiyaClient } from '../src/client.js';
import { GetPatentValuationInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/zhihuiya/scenarios.json', import.meta.url));
const connection = { provider: 'zhihuiya', name: 'zhihuiya' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZhihuiya));

async function invokeZhihuiya(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getPatentValuation') throw new Error(`unknown Zhihuiya operation ${scenario.operation}`);
  const client = new ZhihuiyaClient(scenario.name === 'getPatentValuation-oversized' ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, { apiKey: new SecretString('sk-private') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'zhihuiya-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'zhihuiya', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getPatentValuation().invoke(call, GetPatentValuationInputSchema.parse(scenario.input));
}
