// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  CRMObjectChangeCreated, CRMObjectChangeUpdated, UIObjectTypePickerPortObjectType, UIUnitObjectTypePicker, defineCRMObjectChangedTriggerBinding, newGetObjectStep,
  type CRMObject, type CRMObjectChangeKind, type CRMObjectChangedEvent, type Connection, type GetObjectInput,
} from '@superdurable/connector-hubspot';

/** The static Dex Web connection for HubSpot. */
export const ConnectionName = 'hubspot-crm-watcher';
/** The stable crmObjectChanged binding that starts this Flow. */
export const CRMObjectChangedTriggerBinding = 'crm-object-changes';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered change finds its existing Flow. */
export const FlowIDPrefix = 'hubspot-crm-object-change-';
/**
 * The properties each delivery and read-back returns; HubSpot ignores a name the object type does not define, so
 * one list serves contacts, companies, and deals.
 */
export const DeliveredProperties = ['email', 'firstname', 'lastname', 'name', 'domain', 'dealname', 'dealstage', 'pipeline', 'hubspot_owner_id'];
const readChangedObjectStepType = 'ReadChangedObject';

/** One crmObjectChanged delivery. */
export interface CRMObjectChange {
  /** The stable Trigger event ID that also names the Flow. */ triggerEventId: string;
  /** The delivered change. */ change: CRMObjectChangedEvent;
}

/** The Flow result and the value of its outcome Attribute. */
export interface ChangeOutcome {
  /** The watched object type. */ objectType: string;
  /** The changed record. */ objectId: string;
  /** The delivered change kind. */ changeKind: CRMObjectChangeKind;
  /** The record read back from HubSpot. */ current: CRMObject;
}

/** The delivered change. */
export const crmObjectChangeAttribute = new Attribute<CRMObjectChange>('hubspot-crm-object-change', jsonCodec<CRMObjectChange>());
/** The record read back from HubSpot. */
export const crmObjectChangeOutcomeAttribute = new Attribute<ChangeOutcome>('hubspot-crm-object-change-outcome', jsonCodec<ChangeOutcome>());

/** Records one HubSpot CRM record change: it reads the record back and stores its current state. */
export class CRMObjectChangeFlow implements Flow<CRMObjectChange> {
  private readonly recordChange = new RecordCRMObjectChange();
  private readonly completeChange = new CompleteCRMObjectChange();
  /** Bind the HubSpot connection that reads changed records. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'HubSpotCRMObjectChangeTypeScript'; }
  /** Register the record, read, and outcome Steps. */
  getSteps(): StepList<CRMObjectChange> {
    return StepList.startStep(this.recordChange).otherSteps(
      newGetObjectStep<CRMObjectChange>({
        stepType: readChangedObjectStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed record back, so the outcome holds its current values rather than the search snapshot.' },
        mapToOperationInput: mapToGetObjectInput,
        found: this.completeChange,
      }),
      this.completeChange,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [crmObjectChangeAttribute, crmObjectChangeOutcomeAttribute] }; }
  /** Declare the crmObjectChanged binding and its object type picker for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineCRMObjectChangedTriggerBinding({ connectionName: ConnectionName, bindingName: CRMObjectChangedTriggerBinding, configurationUI: { units: [{
      id: 'watchedObjectType', unitId: UIUnitObjectTypePicker, label: 'Watched object type', required: true,
      description: "Choose whether created and changed contacts, companies, or deals start this Flow; the unit saves HubSpot's object name. The connection's private app or OAuth app needs the matching crm.objects.<type>.read scope, which the HubSpot authorization guide already lists. It cannot be blank. The first start records existing records as a baseline and starts no Flow for them. Restart the Worker after saving; a changed object type records a new baseline.",
      bindings: [{ port: UIObjectTypePickerPortObjectType, jsonPointer: '/objectType' }],
    }] } })];
  }
}

/** Admit every change; an application narrows it, for example to created records. */
export function acceptEveryCRMObjectChange(): boolean { return true; }

/** Name one Flow per record version; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<CRMObjectChangedEvent>): string { return FlowIDPrefix + event.id; }

/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<CRMObjectChangedEvent>): CRMObjectChange { return { triggerEventId: event.id, change: event.payload }; }

/** Read the changed record with the delivered properties. */
export function mapToGetObjectInput(input: CRMObjectChange): GetObjectInput {
  return { objectType: input.change.objectType, objectId: input.change.object.id, properties: DeliveredProperties };
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered record change, then read the record back."
/** Validate and persist the delivered change. */
export class RecordCRMObjectChange implements Step<CRMObjectChange> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordCRMObjectChange'; }
  /** Store the change and read the record back. */
  execute(context: Context, input: CRMObjectChange): StepDecision {
    if (!input.triggerEventId?.trim()) return forceFail('triggerEventId is required');
    if (!input.change?.objectType || !input.change.object?.id?.trim()) return forceFail('change.objectType and change.object.id are required');
    if (input.change.changeKind !== CRMObjectChangeCreated && input.change.changeKind !== CRMObjectChangeUpdated) return forceFail('change.changeKind must be created or updated');
    crmObjectChangeAttribute.set(context, input);
    return goTo(new StepRef(readChangedObjectStepType), input);
  }
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the record read back from HubSpot as the outcome and complete the Flow."
/** Persist the record read back and complete. */
export class CompleteCRMObjectChange implements Step<QueryResult<CRMObject>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteCRMObjectChange'; }
  /** Store the outcome and complete the Flow with it. */
  execute(context: Context, result: QueryResult<CRMObject>): StepDecision {
    const change = crmObjectChangeAttribute.get(context);
    const outcome: ChangeOutcome = { objectType: change.change.objectType, objectId: change.change.object.id, changeKind: change.change.changeKind, current: result.value };
    crmObjectChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
