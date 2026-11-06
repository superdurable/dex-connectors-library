// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetEnvelopeStep, type Connection, type Envelope, type GetEnvelopeInput } from '@superdurable/connector-docusign';

/** Static DocuSign connection selected in Dex Web. */
export const ConnectionName = 'docusign-envelope-reader';
/** Last envelope read, persisted as safe provider metadata. */
export const envelopeStatus = new Attribute<Envelope>('docusign-envelope-status', jsonCodec<Envelope>());

/** Read one envelope through the DocuSign connector. */
export class EnvelopeStatusFlow implements Flow<GetEnvelopeInput> {
  private readonly prepare = new PrepareEnvelopeStatus();
  private readonly complete = new CompleteEnvelopeStatus();
  /** Bind one configured DocuSign connection. */
  constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */
  getFlowType(): string { return 'DocuSignEnvelopeStatusFlow'; }
  /** Register the provider read and completion Steps. */
  getSteps(): StepList<GetEnvelopeInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetEnvelopeStep({
        stepType: 'ReadDocuSignEnvelope',
        annotations: { groupId: 'docusign-envelope', groupLabel: 'DocuSign Envelope', explanation: 'Read one envelope status and its safe custom fields.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the envelope Attribute in the Flow persistence schema. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [envelopeStatus] }; }
}

/** Validate the envelope identity before a provider request. */
// dex:group group-id:docusign-envelope group-label:"DocuSign Envelope"
// dex:explanation text:"Validate the DocuSign envelope ID before reading its status."
export class PrepareEnvelopeStatus implements Step<GetEnvelopeInput> {
  /** Stable Step identity. */
  getStepType(): string { return 'PrepareDocuSignEnvelopeStatus'; }
  /** Route a valid envelope ID to the provider read. */
  execute(_context: Context, input: GetEnvelopeInput): StepDecision {
    if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(input.envelopeId)) return forceFail('DocuSign envelope ID is invalid');
    return goTo(new StepRef('ReadDocuSignEnvelope'), input);
  }
}

/** Persist a completed DocuSign envelope read. */
// dex:group group-id:docusign-envelope group-label:"DocuSign Envelope"
// dex:explanation text:"Persist the envelope status and complete the Flow."
export class CompleteEnvelopeStatus implements Step<QueryResult<Envelope>> {
  /** Stable Step identity. */
  getStepType(): string { return 'CompleteDocuSignEnvelopeStatus'; }
  /** Save safe status metadata and complete. */
  execute(context: Context, result: QueryResult<Envelope>): StepDecision {
    envelopeStatus.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
