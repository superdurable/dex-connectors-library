// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WeibanClient } from '../src/client.js';
import { ListTagsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/weiban/scenarios.json', import.meta.url));
const connection = { provider: 'weiban', name: 'weiban' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWeiban));

async function invokeWeiban(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listTags') throw new Error(`unknown Weiban operation ${scenario.operation}`);
  const credentials = { corpId: scenario.name.endsWith('-defect') ? '' : 'corp_test', secret: new SecretString('REDACTED_WEIBAN_SECRET') };
  const client = new WeibanClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'weiban-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'weiban', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listTags().invoke(call, ListTagsInputSchema.parse(scenario.input));
}
