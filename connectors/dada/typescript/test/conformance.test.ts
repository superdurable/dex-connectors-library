// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { DadaClient, signRequest } from '../src/client.js';
import { QueryOrderStatusInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/dada/scenarios.json', import.meta.url));
const connection = { provider: 'dada', name: 'dada' };
it('matches the official uppercase MD5 signature vector', () => {
  expect(signRequest('abcdefg123', '123456', '{"order_id":"20170301000001"}', '73753', '1488363493')).toBe('71CEE2D6544277940D8C72979E71D0E1');
});
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDada, ''));

async function invokeDada(scenario: Scenario, recorder: ExchangeRecorder) {
  const isMissing = scenario.name === 'queryOrderStatus-missing-credentials';
  const credentials = { appKey: isMissing ? '' : '123456', appSecret: new SecretString(isMissing ? '' : 'abcdefg123'), sourceID: isMissing ? '' : '73753' };
  const client = new DadaClient(scenario.name === 'queryOrderStatus-oversize' ? { maxResponseBytes: 5 } : {},
    new StaticCredentialProvider([[connection, credentials]]), { now: () => new Date(1488363493 * 1000), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'dada-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'dada', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.queryOrderStatus().invoke(call, QueryOrderStatusInputSchema.parse(scenario.input));
}
