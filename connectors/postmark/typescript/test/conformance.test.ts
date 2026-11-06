// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { PostmarkClient } from '../src/client.js';
import { GetMessageDetailsInputSchema, ListOutboundMessagesInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/postmark/scenarios.json', import.meta.url));
const connection = { provider: 'postmark', name: 'postmark' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePostmark));

async function invokePostmark(scenario: Scenario, recorder: ExchangeRecorder) {
  return withScenarioHTTPServer(recorder, async baseURL => {
    const client = new PostmarkClient({ baseUrl: baseURL }, new StaticCredentialProvider([
      [connection, { apiKey: new SecretString('test-secret-token') }],
    ]));
    const call: Call = {
      context: { flowId: 'postmark-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'postmark', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    if (scenario.operation === 'listOutboundMessages') return client.listOutboundMessages().invoke(call, ListOutboundMessagesInputSchema.parse(scenario.input));
    if (scenario.operation === 'getMessageDetails') return client.getMessageDetails().invoke(call, GetMessageDetailsInputSchema.parse(scenario.input));
    throw new Error(`unknown Postmark operation ${scenario.operation}`);
  });
}
