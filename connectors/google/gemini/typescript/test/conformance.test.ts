// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { GeminiClient } from '../src/client.js';
import { GenerateContentRequestSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/gemini/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'gemini-default' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGemini));

async function invokeGemini(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'generateContent') throw new Error(`unknown Gemini operation ${scenario.operation}`);
  {
    const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
    const client = new GeminiClient({ endpoint: 'http://127.0.0.1:12345/v1beta' }, {
      resolve: async () => ({ apiKey: new SecretString('AIzaSENTINEL-test-key_0123456789') }),
    }, { send: sender });
    const call = scenarioCall(scenario, connection, 'gemini', {
      flowId: 'gemini-conformance', cancellationSignal: new AbortController().signal,
    }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
    return client.generateContent().invoke(call, GenerateContentRequestSchema.parse(scenario.input));
  }
}
