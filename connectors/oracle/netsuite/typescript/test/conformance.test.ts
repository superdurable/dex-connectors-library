// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OracleNetSuiteClient } from '../src/client.js';
import { GetCustomerInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/oracle/netsuite/scenarios.json', import.meta.url));
const connection = { provider: 'oracle-netsuite', name: 'oracle-netsuite' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOracleNetSuite, ''));

async function invokeOracleNetSuite(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-oracle-netsuite-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new OracleNetSuiteClient({
    baseUrl: 'http://127.0.0.1:12345',
    authorizationEndpoint: 'http://127.0.0.1:12345/authorize',
    tokenEndpoint: 'http://127.0.0.1:12345/token',
  }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'oracle-netsuite-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'oracle-netsuite', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getCustomer') return client.getCustomer().invoke(call, GetCustomerInputSchema.parse(scenario.input));
  throw new Error(`unknown OracleNetSuite operation ${scenario.operation}`);
}
