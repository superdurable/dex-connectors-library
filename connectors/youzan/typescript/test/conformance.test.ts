// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { YouzanClient } from '../src/client.js';
import type { GetShopBasicInput, Credentials } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/youzan/scenarios.json', import.meta.url));
const connection = { provider: 'youzan', name: 'shop-basic' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYouzan));

async function invokeYouzan(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = async (request: ProviderRequest) => {
    const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
    if (response.body.length > request.maxResponseBytes!) throw new BodyTooLargeError(response.status, response.headers);
    return response;
  };
  const credentials: Credentials = { accessToken: new SecretString('sample-test-token') };
  const source = new StaticCredentialProvider(scenario.name === 'getShopBasic-defect' ? [] : [[connection, credentials]]);
  const config = { baseUrl: 'http://127.0.0.1', ...(scenario.name === 'getShopBasic-oversized' ? { maxResponseBytes: 16 } : {}) };
  const client = new YouzanClient(config, source, { send });
  const call = scenarioCall(scenario, connection, 'youzan', {
    flowId: 'youzan-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'getShopBasic') throw new Error(`unknown Youzan operation ${scenario.operation}`);
  return client.getShopBasic().invoke(call, scenario.input as unknown as GetShopBasicInput);
}
