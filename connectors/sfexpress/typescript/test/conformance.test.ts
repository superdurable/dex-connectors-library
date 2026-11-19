// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SFExpressClient } from '../src/client.js';
import { SyntheticRandomSource, answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/sfexpress/scenarios.json', import.meta.url));
const connection = { provider: 'sfexpress', name: 'sfexpress' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSFExpress, ''));
async function invokeSFExpress(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'queryRoutes') throw new Error(`unknown operation ${scenario.operation}`);
  const invalid = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { partnerId: invalid ? '' : 'customer-1', checkword: new SecretString(invalid ? '' : 'private checkword') }]]);
  const randomSource = new SyntheticRandomSource(scenario.randomSeed ?? 'SYNTHETIC_CONFORMANCE_sfexpress_defect');
  const client = new SFExpressClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}, credentials, { now: () => new Date(1731510000 * 1000), random: count => randomSource.read(count), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'sfexpress-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'sfexpress', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.queryRoutes().invoke(call, scenario.input as unknown as Parameters<ReturnType<SFExpressClient['queryRoutes']>['invoke']>[1]);
}
