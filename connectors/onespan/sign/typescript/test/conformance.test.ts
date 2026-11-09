// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OneSpanSignClient } from '../src/client.js';
import { GetPackageInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/onespan/sign/scenarios.json', import.meta.url));
const connection = { provider: 'onespan-sign', name: 'onespan-sign' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOneSpanSign, '/api'));

async function invokeOneSpanSign(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-onespan-sign-api-key') }]];
  const client = new OneSpanSignClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'onespan-sign-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'onespan-sign', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getPackage') return client.getPackage().invoke(call, GetPackageInputSchema.parse(scenario.input));
  throw new Error(`unknown OneSpanSign operation ${scenario.operation}`);
}
