// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { OpenAIClient } from '../src/client.js';
import { CreateRequestSchema, RetrieveRequestSchema, type Response } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/openai/scenarios.json', import.meta.url));
const connection = { provider: 'openai', name: 'openai' };
const noProgress = { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOpenAI, '/v1'));

async function invokeOpenAI(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new OpenAIClient({ endpoint: 'http://127.0.0.1:12345/v1' }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-key') }),
  }, { send, now: () => new Date('2026-11-02T00:00:00Z') });
  const call = scenarioCall(scenario, connection, 'openai', context, noProgress) as Call;
  if (scenario.operation === 'createResponse') {
    const operation = client.createResponse();
    const input = CreateRequestSchema.parse(scenario.input);
    return invokeProductionMutation(operation, scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<typeof input, Response>, call, input), error => error instanceof RetryError);
  }
  if (scenario.operation === 'retrieveResponse') return client.retrieveResponse().invoke(call, RetrieveRequestSchema.parse(scenario.input));
  throw new Error(`unknown OpenAI operation ${scenario.operation}`);
}
