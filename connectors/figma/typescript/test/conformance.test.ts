// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { FigmaClient } from '../src/client.js';
import { GetFileInputSchema, GetFileCommentsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/figma/scenarios.json', import.meta.url));

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFigma));

async function invokeFigma(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new FigmaClient({ baseUrl: 'http://127.0.0.1:12345/v1' }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-key') }),
  }, { send: sender });
  const call = scenarioCall(scenario, { provider: 'figma', name: 'figma' }, 'figma', {
    flowId: 'figma-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'getFile') return client.getFile().invoke(call, GetFileInputSchema.parse(scenario.input));
  if (scenario.operation === 'getFileComments') return client.getFileComments().invoke(call, GetFileCommentsInputSchema.parse(scenario.input));
  throw new Error(`unknown Figma operation ${scenario.operation}`);
}
