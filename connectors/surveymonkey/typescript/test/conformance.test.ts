// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SurveyMonkeyClient } from '../src/client.js';
import { GetResponseInputSchema, ListSurveysInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/surveymonkey/scenarios.json', import.meta.url));
const connection = { provider: 'surveymonkey', name: 'surveymonkey' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSurveyMonkey, '/v3'));

async function invokeSurveyMonkey(scenario: Scenario, recorder: ExchangeRecorder) {
  const client = new SurveyMonkeyClient({ baseUrl: 'http://127.0.0.1:12345/v3' }, new StaticCredentialProvider([[connection, { accessToken: new SecretString('test-token') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'surveymonkey-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'surveymonkey', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listSurveys') return client.listSurveys().invoke(call, ListSurveysInputSchema.parse(scenario.input));
  if (scenario.operation === 'getResponse') return client.getResponse().invoke(call, GetResponseInputSchema.parse(scenario.input));
  throw new Error(`unknown SurveyMonkey operation ${scenario.operation}`);
}
