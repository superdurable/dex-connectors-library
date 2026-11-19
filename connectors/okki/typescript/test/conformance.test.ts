// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OkkiClient } from '../src/client.js';
import { ListCustomerFieldsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/okki/scenarios.json', import.meta.url));
const connection = { provider: 'okki', name: 'okki' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOkki, ''));

async function invokeOkki(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, scenario.name.endsWith("defect") ? { clientId: "", clientSecret: new SecretString(""), accessToken: new SecretString("") } : { clientId: "client123", clientSecret: new SecretString("secretKey"), accessToken: new SecretString("jwt123") }]]);
  const client = new OkkiClient({}, credentials, {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'okki-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'okki', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listCustomerFields().invoke(call, ListCustomerFieldsInputSchema.parse(scenario.input));
}
