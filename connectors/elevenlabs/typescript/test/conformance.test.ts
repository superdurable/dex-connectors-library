// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { ElevenLabsClient } from '../src/client.js';
import { CreateSpeechInputSchema, ListVoicesInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/elevenlabs/scenarios.json', import.meta.url));
const connection = { provider: 'elevenlabs', name: 'elevenlabs' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeElevenLabs));

async function invokeElevenLabs(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new ElevenLabsClient({ baseUrl: 'http://127.0.0.1' }, {
    resolve: async () => ({ apiKey: new SecretString('test-key-123') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'elevenlabs', {
    flowId: 'conformance-flow', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listVoices') return client.listVoices().invoke(call, ListVoicesInputSchema.parse(scenario.input));
  if (scenario.operation === 'createSpeech') return client.createSpeech().invoke(call, CreateSpeechInputSchema.parse(scenario.input));
  throw new Error(`unknown ElevenLabs operation ${scenario.operation}`);
}
