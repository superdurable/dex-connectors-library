// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  EmployeeChangeActionDeleted, EmployeeChangeActionInserted, EmployeeChangeActionUpdated, defineEmployeeChangedTriggerBinding, newGetEmployeeStep,
  type Connection, type EmployeeChangedEvent, type EmployeeRecord, type GetEmployeeInput,
} from '@superdurable/connector-bamboohr';

/** Static Dex Web connection for BambooHR. */
export const ConnectionName = 'bamboohr-company';
/** The stable employeeChanged binding that starts this Flow. */
export const EmployeeChangedTriggerBinding = 'employee-changes';
/** The fields read back for an inserted or updated employee, because a change carries no field values. */
export const HandOffFields = ['firstName', 'lastName', 'workEmail', 'status', 'department', 'jobTitle', 'terminationDate'];

/** One employeeChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface EmployeeChangeInput { /** Names the Flow too. */ triggerEventId: string; /** The delivered change. */ change: EmployeeChangedEvent }
/** The Flow result: the changed employee and, unless it is gone, its hand-off fields. */
export interface EmployeeChangeOutcome {
  /** The changed employee. */ employeeId: string;
  /** The delivered action. */ action: string;
  /** BambooHR's change instant. */ lastChanged: string;
  /** Read back from BambooHR; absent for a deleted employee. */ current?: EmployeeRecord;
  /** Whether BambooHR no longer returns the employee. */ isGone?: boolean;
}

/** The delivered change. */
export const employeeChangeAttribute = new Attribute<EmployeeChangeInput>('bamboohr-employee-change', jsonCodec<EmployeeChangeInput>());
/** The recorded outcome. */
export const employeeChangeOutcomeAttribute = new Attribute<EmployeeChangeOutcome>('bamboohr-employee-change-outcome', jsonCodec<EmployeeChangeOutcome>());

/** Records one BambooHR employee change and reads an inserted or updated employee back. */
export class EmployeeChangeFlow implements Flow<EmployeeChangeInput> {
  private readonly record = new RecordEmployeeChange();
  private readonly complete = new CompleteEmployeeChange();
  /** Bind the static BambooHR connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError(`employee change Flow requires the ${ConnectionName} connection`);
  }
  /** Stable Flow type. */ getFlowType(): string { return 'BambooHREmployeeChangeTypeScript'; }
  /** Start at the record Step, which the Trigger starts the Flow at. */
  getSteps(): StepList<EmployeeChangeInput> {
    return StepList.startStep(this.record).otherSteps(newGetEmployeeStep<EmployeeChangeInput>({
      stepType: 'ReadChangedEmployee', connection: this.connection, connectionName: ConnectionName,
      annotations: { groupId: 'change', groupLabel: 'Change', explanation: "Read the changed employee's hand-off fields back from BambooHR, since the change carries no field values." },
      mapToOperationInput: mapToGetEmployeeInput, found: this.complete,
      // Business reaction: an employee deleted after the change is recorded as gone.
      notFound: this.complete,
    }), this.complete);
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [employeeChangeAttribute, employeeChangeOutcomeAttribute] }; }
  /** Declare the employeeChanged binding and its change type picker. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineEmployeeChangedTriggerBinding({
      connectionName: ConnectionName, bindingName: EmployeeChangedTriggerBinding,
      configurationUI: { units: [{
        id: 'changeType', unitId: 'changeTypePicker', label: 'Employee changes', required: false,
        description: "Choose which BambooHR employee changes start this Flow: Inserted for new hires, Updated for record, job, compensation, and employment status changes including terminations, Deleted for removed records, or Every change. The unit saves inserted, updated, or deleted; leaving it unsaved or choosing Every change starts a Flow for every change. The changes come from the employees the connection's API key user can see, so no BambooHR page needs a value. The first poll starts at the current time and earlier changes never start Flows; restart the Worker after saving, and a changed choice starts again from the current time.",
        bindings: [{ port: 'changeType', jsonPointer: '/changeType' }],
      }] },
    })];
  }
}

/** Name one Flow per change, so a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<EmployeeChangedEvent>): string { return `bamboohr-employee-change-${event.id}`; }
/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<EmployeeChangedEvent>): EmployeeChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Admit every change; the binding's saved change type already narrows the list. */
export function acceptEveryEmployeeChange(): boolean { return true; }
/** Read the changed employee's hand-off fields. */
export function mapToGetEmployeeInput(input: EmployeeChangeInput): GetEmployeeInput { return { employeeId: input.change.employeeId, fields: HandOffFields }; }

/** Return why a delivered change is unusable, or '' when it is valid. */
export function validateEmployeeChangeInput(input: EmployeeChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.employeeId?.trim() || !input.change.lastChanged || input.change.lastChanged.startsWith('0001-01-01T')) return 'change.employeeId and change.lastChanged are required';
  if (![EmployeeChangeActionInserted, EmployeeChangeActionUpdated, EmployeeChangeActionDeleted].includes(input.change.action)) return 'change.action must be Inserted, Updated, or Deleted';
  return '';
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the change; complete at once for a deleted employee, otherwise read it back."
class RecordEmployeeChange implements Step<EmployeeChangeInput> {
  getStepType(): string { return 'RecordEmployeeChange'; }
  execute(context: Context, input: EmployeeChangeInput): StepDecision {
    const problem = validateEmployeeChangeInput(input);
    if (problem) return forceFail(problem);
    employeeChangeAttribute.set(context, input);
    const { employeeId, action, lastChanged } = input.change;
    if (action === EmployeeChangeActionDeleted) {
      const outcome: EmployeeChangeOutcome = { employeeId, action, lastChanged, isGone: true };
      employeeChangeOutcomeAttribute.set(context, outcome);
      return gracefulComplete(outcome);
    }
    return goTo(new StepRef('ReadChangedEmployee'), input);
  }
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the employee read back, or mark it gone when deleted first, and complete."
class CompleteEmployeeChange implements Step<QueryResult<EmployeeRecord>> {
  getStepType(): string { return 'CompleteEmployeeChange'; }
  execute(context: Context, result: QueryResult<EmployeeRecord>): StepDecision {
    const { employeeId, action, lastChanged } = employeeChangeAttribute.get(context).change;
    const outcome: EmployeeChangeOutcome = { employeeId, action, lastChanged, ...(result.branch === 'found' ? { current: result.value } : { isGone: true }) };
    employeeChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
