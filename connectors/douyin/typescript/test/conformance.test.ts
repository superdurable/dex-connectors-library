// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { DouyinClient } from '../src/client.js';
import { GetBehaviorSummaryInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/douyin/scenarios.json', import.meta.url));
const connection = { provider: 'douyin', name: 'douyin' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDouyin, ''));

async function invokeDouyin(scenario: Scenario, recorder: ExchangeRecorder) {
  const clientSecret = scenario.name.endsWith('missing-secret') ? '' : 'test-client-secret';
  const credentials: Array<[typeof connection, { clientKey: string; clientSecret: SecretString }]> = [[connection, { clientKey: 'tt-test-app', clientSecret: new SecretString(clientSecret) }]];
  const client = new DouyinClient({ maxResponseBytes: scenario.name.endsWith('token-oversize') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date('2026-11-16T04:00:00Z'),
  });
  const call: Call = {
    context: { flowId: 'douyin-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'douyin', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getBehaviorSummary') return client.getBehaviorSummary().invoke(call, GetBehaviorSummaryInputSchema.parse(scenario.input));
  throw new Error(`unknown Douyin operation ${scenario.operation}`);
}
