// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { LinkedInClient, MinimumPollIntervalMs, type LinkedInClientOptions, type LinkedInResponse } from './client.js';
export { GetAuthenticatedProfileOperation } from './operations.js';
export {
  ProfileChangedEventSchema, ProfileChangedTriggerConfigurationSchema, ProfileClaimsSchema, ProfileFieldEmail, ProfileFieldEmailVerified, ProfileFieldFamilyName,
  ProfileFieldGivenName, ProfileFieldLocale, ProfileFieldName, ProfileFieldPictureURL, ProfileFieldSchema,
  type ProfileChangedEvent, type ProfileChangedTriggerConfiguration, type ProfileClaims, type ProfileField,
} from './profile_changed.js';
export {
  ProfileChangedTriggerDefinition, decodeProfileChangedEventPayload, defineProfileChangedTriggerBinding, newProfileChangedTrigger, newProfileTriggerRunner, newProjectProfileTriggerRunner,
  type ProfileChangedTriggerBindingConfig, type ProfileChangedTriggerConfig, type ProfileChangedTriggerRoute, type ProfileTriggerRunnerOptions, type ProjectProfileChangedTriggerRoute,
} from './trigger.js';
