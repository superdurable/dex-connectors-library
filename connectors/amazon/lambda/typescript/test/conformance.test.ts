// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { AmazonLambdaClient } from '../src/client.js';
import { GetFunctionInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/amazon/lambda/scenarios.json', import.meta.url));
const connection = { provider: 'amazon-lambda', name: 'amazon-lambda' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAmazonLambda, ''));

async function invokeAmazonLambda(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessKeyId: SecretString; secretAccessKey: SecretString; sessionToken: SecretString }]> = [[connection, { accessKeyId: new SecretString('AKIALAMBDACONNECTOR'), secretAccessKey: new SecretString('lambda-test-secret'), sessionToken: new SecretString('') }]];
  const client = new AmazonLambdaClient({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date('2026-10-30T00:00:00Z'),
  });
  const call: Call = {
    context: { flowId: 'lambda-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'amazon-lambda', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getFunction') return client.getFunction().invoke(call, GetFunctionInputSchema.parse(scenario.input));
  throw new Error(`unknown Amazon Lambda operation ${scenario.operation}`);
}
