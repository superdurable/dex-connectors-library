// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { AmazonRedshiftClient } from '../src/client.js';
import { GetStatementResultInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/amazon/redshift/scenarios.json', import.meta.url));
const connection = { provider: 'amazon-redshift', name: 'amazon-redshift' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAmazonRedshift, ''));

async function invokeAmazonRedshift(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessKeyId: SecretString; secretAccessKey: SecretString; sessionToken: SecretString }]> = [[connection, { accessKeyId: new SecretString('AKIAREDSHIFTCONNECTOR'), secretAccessKey: new SecretString('redshift-test-secret'), sessionToken: new SecretString('') }]];
  const client = new AmazonRedshiftClient({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date('2026-10-30T00:00:00Z'),
  });
  const call: Call = {
    context: { flowId: 'redshift-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'amazon-redshift', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getStatementResult') return client.getStatementResult().invoke(call, GetStatementResultInputSchema.parse(scenario.input));
  throw new Error(`unknown Amazon Redshift operation ${scenario.operation}`);
}
