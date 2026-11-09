// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MarketoClient } from '../src/client.js';
import { GetLeadInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/adobe/marketo/scenarios.json', import.meta.url));
const connection = { provider: 'adobe', name: 'adobe-marketo' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMarketo, '/rest'));

async function invokeMarketo(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { clientId: string; clientSecret: SecretString; accessToken: SecretString }]> = [[connection, { clientId: 'sample-client-id', clientSecret: new SecretString('sample-client-secret'), accessToken: new SecretString('sample-access-token') }]];
  const client = new MarketoClient({
    restEndpoint: 'https://123-ABC-456.mktorest.com/rest',
    tokenEndpoint: 'https://123-ABC-456.mktorest.com/identity/oauth/token',
  }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'marketo-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'adobe-marketo', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getLead') return client.getLead().invoke(call, GetLeadInputSchema.parse(scenario.input));
  throw new Error(`unknown Marketo operation ${scenario.operation}`);
}
