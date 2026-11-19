// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GetuiClient } from '../src/client.js';
import { GetDailyPushReportInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/getui/scenarios.json', import.meta.url));
const connection = { provider: 'getui', name: 'getui' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGetui, ''));

async function invokeGetui(scenario: Scenario, recorder: ExchangeRecorder) {
  const appId = scenario.name.endsWith('defect') ? '' : 'test-app';
  const credentials = new StaticCredentialProvider([[connection, { appId, appKey: new SecretString('test-appkey'), masterSecret: new SecretString('test-mastersecret') }]]);
  const client = new GetuiClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 16 : 1_048_576 }, credentials, {
    now: () => new Date('2026-01-01T00:00:00Z'),
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'getui-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'getui', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getDailyPushReport') return client.getDailyPushReport().invoke(call, GetDailyPushReportInputSchema.parse(scenario.input));
  throw new Error('unknown Getui operation ' + scenario.operation);
}
