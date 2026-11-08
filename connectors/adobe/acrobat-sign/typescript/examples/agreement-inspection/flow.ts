// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetAgreementStep, type Connection, type AgreementStatus, type GetAgreementInput,
} from '@superdurable/connector-adobe-acrobat-sign';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'adobe-acrobat-sign';
const agreementAttribute = new Attribute<AgreementStatus>('adobe-acrobat-sign-agreement', jsonCodec<AgreementStatus>());

/** Read one agreement through a typed AcrobatSign Query. */
export class AgreementInspectionFlow implements Flow<GetAgreementInput> {
  private readonly complete = new CompleteAgreementInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('AcrobatSign connection name differs');
    this.read = newGetAgreementStep<GetAgreementInput>({
      stepType: 'CallGetAgreement',
      annotations: { groupId: 'provider', groupLabel: 'AcrobatSign', explanation: 'Read one agreement from AcrobatSign.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteAcrobatSign'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'AcrobatSignAgreementInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetAgreementInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected agreement result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [agreementAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected agreement and complete the Flow."
class CompleteAgreementInspection implements Step<QueryResult<AgreementStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteAcrobatSign'; }
  /** Persist and return the selected agreement. */
  execute(context: Context, result: QueryResult<AgreementStatus>): StepDecision {
    agreementAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
