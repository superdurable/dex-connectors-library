// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WxPusherClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/wxpusher/scenarios.json', import.meta.url));
const connection = { provider: 'wxpusher', name: 'wxpusher' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWxPusher, ''));

async function invokeWxPusher(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getSubscriberCount') throw new Error(`unknown WxPusher operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appToken: new SecretString(isDefect ? '' : 'AT_dummy') }]]);
  const client = new WxPusherClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'wxpusher-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'wxpusher', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getSubscriberCount().invoke(call, scenario.input as unknown as Parameters<ReturnType<WxPusherClient['getSubscriberCount']>['invoke']>[1]);
}
