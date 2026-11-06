// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, forceFail, goTo, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import { newAppendTextStep, newCreateDocumentStep, newGetDocumentTextStep, newReplaceDocumentTextStep, type AppendTextInput, type CreateDocumentInput, type GetDocumentTextInput, type ReplaceDocumentTextInput, type Connection } from '@superdurable/connector-google-docs';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'policy-docs';

/** One Google Docs request to execute through a named connection. */
export type PolicyDocumentRequest =
  | { operation: 'getDocumentText'; request: GetDocumentTextInput }
  | { operation: 'createDocument'; request: CreateDocumentInput }
  | { operation: 'replaceDocumentText'; request: ReplaceDocumentTextInput }
  | { operation: 'appendText'; request: AppendTextInput };

/** A runnable Flow that exercises every Google Docs operation. */
export class PolicyDocumentsFlow implements Flow<PolicyDocumentRequest> {
  private readonly prepare = new PreparePolicyDocumentRequest();
  private readonly complete = new CompletePolicyDocumentRequest();
  /** Bind all four operations to one static connection. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Google Docs connection is required');
  }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'GooglePolicyDocumentsFlow'; }
  /** Register dispatch, provider, and completion Steps. */
  getSteps(): StepList<PolicyDocumentRequest> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetDocumentTextStep({ stepType: 'ReadGoogleDocument', annotations: { groupId: 'google-policy-documents', groupLabel: 'Policy Documents', explanation: 'Read one bounded Google Docs revision.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (input: GetDocumentTextInput) => input, read: this.complete }),
      newCreateDocumentStep({ stepType: 'CreateGoogleDocument', annotations: { groupId: 'google-policy-documents', groupLabel: 'Policy Documents', explanation: 'Create a document with duplicate-aware recovery.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (input: CreateDocumentInput) => input, created: this.complete }),
      newReplaceDocumentTextStep({ stepType: 'ReplaceGoogleDocumentText', annotations: { groupId: 'google-policy-documents', groupLabel: 'Policy Documents', explanation: 'Replace text at the required document revision.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (input: ReplaceDocumentTextInput) => input, replaced: this.complete }),
      newAppendTextStep({ stepType: 'AppendGoogleDocumentText', annotations: { groupId: 'google-policy-documents', groupLabel: 'Policy Documents', explanation: 'Append text at the required document revision.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (input: AppendTextInput) => input, appended: this.complete }),
      this.complete,
    );
  }
  /** This Flow persists no Attributes or Streams. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

// dex:group group-id:google-policy-documents group-label:"Policy Documents"
// dex:explanation text:"Validate one requested Google Docs operation and route it to the named connection."
class PreparePolicyDocumentRequest implements Step<PolicyDocumentRequest> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareGooglePolicyDocumentRequest'; }
  /** Route only a declared operation with nonempty input. */
  execute(_context: Context, input: PolicyDocumentRequest): StepDecision {
    if (!input || !input.request) return forceFail('a Google Docs request is required');
    switch (input.operation) {
      case 'getDocumentText': return goTo(new StepRef('ReadGoogleDocument'), input.request);
      case 'createDocument': return goTo(new StepRef('CreateGoogleDocument'), input.request);
      case 'replaceDocumentText': return goTo(new StepRef('ReplaceGoogleDocumentText'), input.request);
      case 'appendText': return goTo(new StepRef('AppendGoogleDocumentText'), input.request);
      default: return forceFail('unsupported Google Docs operation');
    }
  }
}

// dex:group group-id:google-policy-documents group-label:"Policy Documents"
// dex:explanation text:"Complete with the provider result after the selected operation succeeds."
class CompletePolicyDocumentRequest implements Step<{ value: unknown }> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteGooglePolicyDocumentRequest'; }
  /** Return the successful provider value. */ execute(_context: Context, result: { value: unknown }): StepDecision { return gracefulComplete(result.value); }
}
