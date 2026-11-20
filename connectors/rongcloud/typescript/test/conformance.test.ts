// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { RongCloudClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, SyntheticRandomSource, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/rongcloud/scenarios.json', import.meta.url));
const connection = { provider: 'rongcloud', name: 'rongcloud' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeRongCloud, ''));
async function invokeRongCloud(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'checkUserOnline') throw new Error(`unknown RongCloud operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appKey: 'REDACTED_APP_KEY', appSecret: new SecretString('REDACTED_APP_SECRET') }]]);
  const client = new RongCloudClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-19T12:00:00Z'), nonce: () => new SyntheticRandomSource(scenario.randomSeed ?? 'SYNTHETIC_CONFORMANCE_RONGCLOUD').read(9) });
  const call: Call = { context: { flowId: 'rongcloud-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'rongcloud', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.checkUserOnline().invoke(call, scenario.input as unknown as Parameters<ReturnType<RongCloudClient['checkUserOnline']>['invoke']>[1]);
}
