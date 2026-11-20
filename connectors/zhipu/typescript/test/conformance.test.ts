// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZhipuClient } from '../src/client.js';
import { GenerateTextInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/zhipu/scenarios.json', import.meta.url));
const connection = { provider: 'zhipu', name: 'zhipu' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZhipu));

async function invokeZhipu(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'generateText') throw new Error(`unknown Zhipu operation ${scenario.operation}`);
  const credentials = { apiKey: new SecretString(scenario.name.endsWith('-defect') ? '' : 'REDACTED_ZHIPU_API_KEY') };
  const client = new ZhipuClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 64 } : {}, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'zhipu-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'zhipu', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.generateText().invoke(call, GenerateTextInputSchema.parse(scenario.input));
}
