// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { AmazonSNSClient } from '../src/client.js';
import { ListTopicsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/amazon/sns/scenarios.json', import.meta.url));
const connection = { provider: 'amazon-sns', name: 'amazon-sns' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAmazonSNS, ''));

async function invokeAmazonSNS(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessKeyId: SecretString; secretAccessKey: SecretString; sessionToken: SecretString }]> = [[connection, { accessKeyId: new SecretString('AKIASNSCONNECTOR'), secretAccessKey: new SecretString('sns-test-secret'), sessionToken: new SecretString('') }]];
  const client = new AmazonSNSClient({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date('2026-10-30T00:00:00Z'),
  });
  const call: Call = {
    context: { flowId: 'sns-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'amazon-sns', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listTopics') return client.listTopics().invoke(call, ListTopicsInputSchema.parse(scenario.input));
  throw new Error(`unknown Amazon SNS operation ${scenario.operation}`);
}
