// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { AcrobatSignClient } from '../src/client.js';
import { GetAgreementInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/adobe/acrobat-sign/scenarios.json', import.meta.url));
const connection = { provider: 'adobe-acrobat-sign', name: 'adobe-acrobat-sign' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAcrobatSign, '/api/rest/v6'));

async function invokeAcrobatSign(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { integrationKey: SecretString }]> = [[connection, { integrationKey: new SecretString('sample-acrobat-sign-integration-key') }]];
  const client = new AcrobatSignClient({ baseUrl: 'http://127.0.0.1:12345/api/rest/v6' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'adobe-acrobat-sign-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'adobe-acrobat-sign', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getAgreement') return client.getAgreement().invoke(call, GetAgreementInputSchema.parse(scenario.input));
  throw new Error(`unknown AcrobatSign operation ${scenario.operation}`);
}
