// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { CohereClient } from '../src/client.js';
import { EmbedInputSchema, RerankInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/cohere/scenarios.json', import.meta.url));
const connection = { provider: 'cohere', name: 'cohere' };
const progress = { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCohere));

async function invokeCohere(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new CohereClient({ baseUrl: 'http://127.0.0.1' }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-key') }),
  }, { send: sender, now: () => new Date('2026-11-02T00:00:00Z') });
  const call = scenarioCall(scenario, connection, 'cohere', {
    flowId: 'cohere-conformance', cancellationSignal: new AbortController().signal,
  }, progress) as Call;
  if (scenario.operation === 'embed') return client.embed().invoke(call, EmbedInputSchema.parse(scenario.input));
  if (scenario.operation === 'rerank') return client.rerank().invoke(call, RerankInputSchema.parse(scenario.input));
  throw new Error(`unknown Cohere operation ${scenario.operation}`);
}
