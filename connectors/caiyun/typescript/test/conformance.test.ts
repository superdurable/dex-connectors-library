// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { CaiyunClient } from '../src/client.js';
import type { Credentials, GetCurrentWeatherInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/caiyun/scenarios.json', import.meta.url));
const connection = { provider: 'caiyun', name: 'current-weather' };
const nonce = Uint8Array.from([0x01, 0x95, 0xc6, 0x8a, 0x42, 0xe7, 0x72, 0x43, 0xbf, 0xf2, 0xac, 0x97, 0xa7, 0x8b, 0x83, 0x7d]);

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCaiyun));

async function invokeCaiyun(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = async (request: ProviderRequest) => {
    const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
    if (response.body.length > request.maxResponseBytes!) throw new BodyTooLargeError(response.status, response.headers);
    return response;
  };
  const credentials: Credentials = { appKey: new SecretString('test-app-key'), appSecret: new SecretString('test-app-secret') };
  const source = new StaticCredentialProvider(scenario.name === 'getCurrentWeather-missing-credentials' ? [] : [[connection, credentials]]);
  const client = new CaiyunClient({ baseUrl: 'http://127.0.0.1', ...(scenario.name === 'getCurrentWeather-oversized' ? { maxResponseBytes: 8 } : {}) }, source, {
    send, now: () => new Date('2026-10-22T07:26:40Z'), nonce: () => nonce,
  });
  const call = scenarioCall(scenario, connection, 'caiyun', {
    flowId: 'caiyun-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'getCurrentWeather') throw new Error(`unknown Caiyun operation ${scenario.operation}`);
  return client.getCurrentWeather().invoke(call, scenario.input as unknown as GetCurrentWeatherInput);
}
