// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JotformClient } from '../src/client.js';
import { GetSubmissionInputSchema, ListFormsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/jotform/scenarios.json', import.meta.url));
const connection = { provider: 'jotform', name: 'jotform' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJotform));

async function invokeJotform(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('test-key') }]];
  const client = new JotformClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'jotform-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'jotform', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listForms') return client.listForms().invoke(call, ListFormsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getSubmission') return client.getSubmission().invoke(call, GetSubmissionInputSchema.parse(scenario.input));
  throw new Error(`unknown Jotform operation ${scenario.operation}`);
}
