// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { BmobClient } from '../src/client.js';
import type { CountClassObjectsInput, Credentials } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/bmob/scenarios.json', import.meta.url));
const connection = { provider: 'bmob', name: 'class-count' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBmob));

async function invokeBmob(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = async (request: ProviderRequest) => {
    const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
    if (response.body.length > request.maxResponseBytes!) throw new BodyTooLargeError(response.status, response.headers);
    return response;
  };
  const credentials: Credentials = { applicationId: new SecretString('test-bmob-app'), restApiKey: new SecretString('test-bmob-rest-key') };
  const source = new StaticCredentialProvider(scenario.name === 'countClassObjects-defect' ? [] : [[connection, credentials]]);
  const config = { baseUrl: 'http://127.0.0.1', ...(scenario.name === 'countClassObjects-oversized' ? { maxResponseBytes: 16 } : {}) };
  const client = new BmobClient(config, source, { send });
  const call = scenarioCall(scenario, connection, 'bmob', {
    flowId: 'bmob-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'countClassObjects') throw new Error(`unknown Bmob operation ${scenario.operation}`);
  return client.countClassObjects().invoke(call, scenario.input as unknown as CountClassObjectsInput);
}
