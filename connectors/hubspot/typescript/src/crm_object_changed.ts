// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';
import { CRMObjectSchema, type CRMObject } from './generated/models.js';
import { searchBody, type ObjectType } from './operations.js';
import { MaximumSearchResults, crmObjectChangePageSize, formatGoTimeMilliseconds, newCRMObjectChangeSearch, parseRFC3339Milliseconds } from './crm_object_change_search.js';

/** HubSpot object type picker UI unit, which stores HubSpot's object name. */ export const UIUnitObjectTypePicker = 'objectTypePicker';
/** Object type picker output port. */ export const UIObjectTypePickerPortObjectType = 'objectType';

/** HubSpot's limit on property names requested by one call. */
const maximumProperties = 100;
/**
 * Bounds CRMObjectChangedTriggerConfiguration.properties; the Trigger always requests the record's creation and
 * last-modified dates beside them.
 */
export const MaximumTriggerProperties = maximumProperties - 2;

/**
 * A record HubSpot created after the newest change an earlier poll of the binding observed, so no earlier
 * delivery or baseline included it.
 */
export const CRMObjectChangeCreated = 'created';
/** Any other change to a record's properties, including a record that appeared in search only after HubSpot's indexing delay. */
export const CRMObjectChangeUpdated = 'updated';
/** The derived kind of one crmObjectChanged delivery. */
export type CRMObjectChangeKind = typeof CRMObjectChangeCreated | typeof CRMObjectChangeUpdated;

/**
 * The crmObjectChanged binding configuration that the object type picker saves in Dex Web. Changing the object
 * type of a binding that already polled makes its next poll record a new baseline for the new object type.
 */
export interface CRMObjectChangedTriggerConfiguration {
  /** The watched object type, contacts, companies, or deals, saved by the object type picker. It is required. */
  objectType: ObjectType;
  /**
   * Internal property names, such as email or dealstage, to return on each delivered record, at most
   * MaximumTriggerProperties. Empty returns HubSpot's default search properties. The Dex Web picker does not
   * set it; an application sets it in code.
   */
  properties?: string[] | null | undefined;
}

/**
 * Decodes a saved crmObjectChanged configuration strictly; unknown fields are rejected. It does not validate
 * values; validateCRMObjectChangedTriggerConfiguration does, as every runner and Trigger factory does.
 */
export const CRMObjectChangedTriggerConfigurationSchema: z.ZodType<CRMObjectChangedTriggerConfiguration> = z.strictObject({
  objectType: z.string().nullable().optional().transform(value => (value ?? '') as ObjectType),
  properties: z.array(z.string()).nullable().optional(),
});

/**
 * Reject a missing or unsupported object type, invalid property names, and a property list whose search request
 * would exceed HubSpot's request body limit, as the Go connector's Validate does.
 */
export function validateCRMObjectChangedTriggerConfiguration(configuration: CRMObjectChangedTriggerConfiguration): void {
  if (!configuration?.objectType) throw new Error('crmObjectChanged objectType is required');
  if (!['contacts', 'companies', 'deals'].includes(configuration.objectType)) throw new Error('crmObjectChanged object type must be contacts, companies, or deals');
  if ((configuration.properties ?? []).length > MaximumTriggerProperties) throw new Error(`crmObjectChanged allows at most ${MaximumTriggerProperties} properties`);
  try { searchBody(newCRMObjectChangeSearch(configuration, 0, String(MaximumSearchResults - crmObjectChangePageSize))); }
  catch (error) { throw new Error(`crmObjectChanged search: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
}

/**
 * One created or changed HubSpot CRM record read through CRM search. Several edits made between two polls
 * arrive as one change carrying the record's latest values. Deleted, archived, and merged-away records are not
 * reported, because CRM search does not return them. Field names match the Go connector.
 */
export interface CRMObjectChangedEvent {
  /** The watched object type. */ objectType: ObjectType;
  /** The derived change kind; see CRMObjectChangeKind. */ changeKind: CRMObjectChangeKind;
  /**
   * The record's last-modified date, lastmodifieddate for contacts and hs_lastmodifieddate otherwise, in UTC with
   * millisecond precision, as Go encodes a time.Time.
   */
  modifiedAt: string;
  /** The record as CRM search returned it, with the configured properties. */ object: CRMObject;
}

/** Decodes a crmObjectChanged payload that this package or the Go connector recorded. */
export const CRMObjectChangedEventSchema: z.ZodType<CRMObjectChangedEvent> = z.object({
  objectType: z.enum(['contacts', 'companies', 'deals']),
  changeKind: z.enum([CRMObjectChangeCreated, CRMObjectChangeUpdated]),
  modifiedAt: z.string().transform((value, context) => {
    const milliseconds = parseRFC3339Milliseconds(value);
    if (milliseconds === undefined) { context.addIssue({ code: 'custom', message: 'modifiedAt must be an RFC 3339 time' }); return z.NEVER; }
    return formatGoTimeMilliseconds(milliseconds);
  }),
  object: CRMObjectSchema,
});

/** Manifest definition of crmObjectChanged. */
export const CRMObjectChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'crmObjectChanged' },
  description: 'Receive each HubSpot contact, company, or deal of one object type that is created or changed, read by polling CRM search on the last-modified date; the first run records a baseline and delivers no existing records.',
};

/** One static application binding of crmObjectChanged. */
export interface CRMObjectChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the objectTypePicker unit. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static crmObjectChanged binding for Flow Definition tooling and Dex Web. */
export function defineCRMObjectChangedTriggerBinding(config: CRMObjectChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: CRMObjectChangedTriggerDefinition, ...config });
}
