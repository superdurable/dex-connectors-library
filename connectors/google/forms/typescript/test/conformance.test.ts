// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { GoogleFormsClient } from '../src/client.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/forms/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'google-forms-intake' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGoogleForms));

async function invokeGoogleForms(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => {
    const fixtureURL = new URL(request.url);
    // Go's conformance adapter removes the client-owned /v1 prefix from fixture paths.
    fixtureURL.pathname = fixtureURL.pathname.replace(/^\/v1(?=\/)/, '');
    return answerRecordedRequest(recorder, { ...request, url: fixtureURL.toString() }, before => new ProviderTransportError(before));
  };
  const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString('forms-token-SENTINEL') }]]);
  const client = new GoogleFormsClient({ endpoint: 'http://127.0.0.1:41801' }, credentials, { send });
  const call = scenarioCall(scenario, connection, 'google-forms', {
    flowId: 'forms-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  switch (scenario.operation) {
    case 'getForm': return client.getForm().invoke(call, scenario.input as { formId: string });
    case 'listResponses': return client.listResponses().invoke(call, scenario.input as { formId: string });
    case 'getResponse': return client.getResponse().invoke(call, scenario.input as { formId: string; responseId: string });
    default: throw new Error(`unknown Google Forms operation ${scenario.operation}`);
  }
}
