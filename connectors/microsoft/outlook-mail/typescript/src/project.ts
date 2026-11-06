// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectionRefSchema } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectCredentialProvider,Connection } from './generated/connection.js';
import { ConnectorID,ProviderID } from './generated/definitions.js';
import { ConfigSchema,decodeCredentials,decodeSharedCredentials,type Credentials } from './generated/models.js';
import { OutlookMailClient,type OutlookMailClientOptions } from './client.js';
/** Encode private fields for atomic credential replacement. */
export function encodeCredentials(credentials:Credentials):Record<string,string>{const fields:Record<string,string>={auth_method:credentials.authMethodId??'microsoft-oauth',client_id:credentials.clientId,client_secret:credentials.clientSecret.reveal()};for(const [key,value] of [['access_token',credentials.accessToken],['refresh_token',credentials.refreshToken]] as const)if(value?.reveal())fields[key]=value.reveal();if(credentials.tenantId)fields.tenant_id=credentials.tenantId;return fields;}
/** Open a statically named Outlook Mail connection from a Dex project. */
export function newProjectConnection(project:LoadedProject,connectionName:string,options:OutlookMailClientOptions={}):Connection{
  if(!project)throw new TypeError('loaded project is required');const reference=ConnectionRefSchema.parse({provider:ProviderID,name:connectionName});const entry=project.configuration.connections.find(value=>value.connectorId===ConnectorID&&value.connectionName===connectionName);if(!entry||entry.provider!==ProviderID)throw new TypeError('Outlook Mail project connection is not configured');const config=ConfigSchema.parse(entry.configuration);
  const credentials=newProjectCredentialProvider(project,connectionName,decodeCredentials,encodeCredentials,decodeSharedCredentials);return new Connection(reference,new OutlookMailClient(config,credentials,options));
}
