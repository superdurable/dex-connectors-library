// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetPackageStep, type Connection, type PackageStatus, type GetPackageInput,
} from '@superdurable/connector-onespan-sign';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'onespan-sign';
const packageAttribute = new Attribute<PackageStatus>('onespan-sign-package', jsonCodec<PackageStatus>());

/** Read one package through a typed OneSpanSign Query. */
export class PackageInspectionFlow implements Flow<GetPackageInput> {
  private readonly complete = new CompletePackageInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('OneSpan Sign connection name differs');
    this.read = newGetPackageStep<GetPackageInput>({
      stepType: 'CallGetPackage',
      annotations: { groupId: 'provider', groupLabel: 'OneSpan Sign', explanation: 'Read one package from OneSpan Sign.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteOneSpanSign'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'OneSpanSignPackageInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetPackageInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected package result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [packageAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected package and complete the Flow."
class CompletePackageInspection implements Step<QueryResult<PackageStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteOneSpanSign'; }
  /** Persist and return the selected package. */
  execute(context: Context, result: QueryResult<PackageStatus>): StepDecision {
    packageAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
