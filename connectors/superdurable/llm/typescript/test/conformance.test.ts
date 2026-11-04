// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it, vi } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import {
  answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario,
} from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { Client } from '../src/client.js';
import { newConnection } from '../src/index.js';
import { GenerateTextRequestSchema } from '../src/generated/models.js';

const active = vi.hoisted(() => ({ recorder: undefined as ExchangeRecorder | undefined }));
vi.mock('@superdurable/connectors-sdk/providerhttp', async importOriginal => ({
  ...await importOriginal<typeof import('@superdurable/connectors-sdk/providerhttp')>(),
  sendProviderRequest: (request: ProviderRequest) => {
    if (!active.recorder) throw new Error('conformance recorder is missing');
    return answerRecordedRequest(active.recorder, request, before => new ProviderTransportError(before));
  },
}));

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/superdurable/llm/scenarios.json', import.meta.url));
const reference = { provider: 'llm', name: 'llm-conformance' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeLLM, '/v1'));

async function invokeLLM(scenario: Scenario, recorder: ExchangeRecorder) {
  active.recorder = recorder;
  const client = new Client({ provider: 'openai', model: 'gpt-6-sol' }, {
    resolve: async () => ({ apiKey: new SecretString('sk-llm-conformance-key') }),
  });
  const call = scenarioCall(scenario, reference, 'llm', {
    flowId: 'llm-conformance-flow', stepExecutionId: 'llm-conformance-step',
    attempt: 1, cancellationSignal: new AbortController().signal, recordHeartbeat: async () => {},
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  const operation = newConnection(client, reference.name).operations.generateText();
  return operation.invoke(call, GenerateTextRequestSchema.parse(scenario.input));
}
