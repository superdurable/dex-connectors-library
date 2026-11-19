// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { CainiaoClient } from '../src/client.js';
import type { Credentials, QueryWaybillTraceInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/cainiao/scenarios.json', import.meta.url));
const connection = { provider: 'cainiao', name: 'waybill-trace' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCainiao));

async function invokeCainiao(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = async (request: ProviderRequest) => {
    const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
    if (response.body.length > request.maxResponseBytes!) throw new BodyTooLargeError(response.status, response.headers);
    return response;
  };
  const credentials: Credentials = { appKey: '123456', appSecret: new SecretString('secretKey'), sessionKey: new SecretString('sessionToken') };
  const source = new StaticCredentialProvider(scenario.name === 'queryWaybillTrace-missing-credentials' ? [] : [[connection, credentials]]);
  const client = new CainiaoClient({ baseUrl: 'http://127.0.0.1/router/rest', ...(scenario.name === 'queryWaybillTrace-oversized' ? { maxResponseBytes: 8 } : {}) }, source, {
    send, now: () => new Date('2026-10-22T07:26:40Z'),
  });
  const call = scenarioCall(scenario, connection, 'cainiao', {
    flowId: 'cainiao-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'queryWaybillTrace') throw new Error(`unknown Cainiao operation ${scenario.operation}`);
  return client.queryWaybillTrace().invoke(call, scenario.input as unknown as QueryWaybillTraceInput);
}
