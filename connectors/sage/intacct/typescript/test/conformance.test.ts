// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SageIntacctClient } from '../src/client.js';
import { GetInvoiceInputSchema, ListInvoicesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/sage/intacct/scenarios.json', import.meta.url));
const connection = { provider: 'sage-intacct', name: 'sage-intacct' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSageIntacct, ''));

async function invokeSageIntacct(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-sage-intacct-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new SageIntacctClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: scenario.name.endsWith('-smallLimit') ? 16 : 1048576 }, new StaticCredentialProvider([[connection, credentials]]), {
    send: async request => {
      const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
      if (request.maxResponseBytes !== undefined && response.body.length > request.maxResponseBytes)
        throw new BodyTooLargeError(response.status, response.headers);
      return response;
    },
  });
  const call: Call = {
    context: { flowId: 'sage-intacct-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'sage-intacct', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getInvoice') return client.getInvoice().invoke(call, GetInvoiceInputSchema.parse(scenario.input));
  if (scenario.operation === 'listInvoices') return client.listInvoices().invoke(call, ListInvoicesInputSchema.parse(scenario.input));
  throw new Error(`unknown Sage Intacct operation ${scenario.operation}`);
}
