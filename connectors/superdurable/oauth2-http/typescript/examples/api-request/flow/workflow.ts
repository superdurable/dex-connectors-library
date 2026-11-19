// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, goTo, gracefulComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newPostNonIdempotentStep, newSendRequestStep, type Connection, type SendRequestOutput } from '@superdurable/connector-superdurable-oauth2-http';

/** Stable Dex Web connection name shared by Worker and starter. */
export const ConnectionName = 'oauth2-http-api';

/** Safe input for one authenticated GET request, or one order POST when orderBody is set. */
export interface RequestInput {
  /** Path under the connection's apiBaseUrl. */ path: string;
  /** Order body sent once as a POST with postNonIdempotent instead of the GET. */ orderBody?: string;
}

/** Request an API path and return its bounded answer. */
export class APIRequestFlow implements Flow<RequestInput> {
  private readonly prepare = new PrepareRequest();
  private readonly complete = new CompleteRequest();

  /** Bind the static provider connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('OAuth 2.0 HTTP connection is required');
  }

  /** Stable Dex Flow type. */
  getFlowType(): string { return 'OAuth2HTTPRequest'; }

  /** Register the preparation, connector, and completion Steps. */
  getSteps(): StepList<RequestInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSendRequestStep({ stepType: 'SendRequest', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'oauth2-http', groupLabel: 'OAuth 2.0 HTTP', explanation: 'Send a GET request to the configured API using its renewable access token.' },
        mapToOperationInput: input => ({ method: 'GET', path: input.path }), completed: this.complete }),
      newPostNonIdempotentStep({ stepType: 'PostOrder', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'oauth2-http', groupLabel: 'OAuth 2.0 HTTP', explanation: 'Send the order once as a POST that is never repeated after it may have reached the API.' },
        mapToOperationInput: input => ({ path: input.path, body: input.orderBody ?? '' }), completed: this.complete }),
      this.complete,
    );
  }
}

/** Route the request to the GET or the order POST. */
// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Read the requested API path, or send the order once when the input has one."
export class PrepareRequest implements Step<RequestInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareOAuth2HTTPRequest'; }
  /** Continue to the connector Step. */
  execute(_context: Context, input: RequestInput): StepDecision {
    if (input.orderBody) return goTo(new StepRef('PostOrder'), input);
    return goTo(new StepRef('SendRequest'), input);
  }
}

/** Complete with the bounded provider answer. */
// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Return the API answer after the connector completes."
export class CompleteRequest implements Step<MutationResult<SendRequestOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteOAuth2HTTPRequest'; }
  /** Return the connector result. */
  execute(_context: Context, result: MutationResult<SendRequestOutput>): StepDecision { return gracefulComplete(result.value); }
}
