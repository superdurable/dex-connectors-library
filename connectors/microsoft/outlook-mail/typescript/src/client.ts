// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import type { CredentialProvider,CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import type { Config,Credentials } from './generated/models.js';
import type { Operations } from './generated/connection.js';
import { newGraphRefreshDriver,type GraphTransport } from './http.js';
import { SearchMessagesOperation,GetMessageOperation,MoveMessageOperation,SetMessageFlagsOperation,type ClientDependencies } from './operations.js';
import { SendMessageOperation,ReplyToMessageOperation } from './send.js';
/** Transport and clock injected for provider tests. */
export interface OutlookMailClientOptions { transport?:GraphTransport; refreshDriver?:CredentialRefreshDriver<Credentials>; now?:()=>Date; requestTimeoutMs?:number }
/** A Microsoft Graph client with six bound Dex operations. */
export class OutlookMailClient implements Operations {
  readonly #dependencies:ClientDependencies;
  /** Validate startup configuration and bind a host credential source. */
  constructor(config:Config,credentials:CredentialProvider<Credentials>,options:OutlookMailClientOptions={}){
    if(!credentials)throw new TypeError('Outlook Mail credential provider is required');const maxResponseBytes=config.maxResponseBytes??4194304;if(!Number.isSafeInteger(maxResponseBytes)||maxResponseBytes<1)throw new TypeError('Microsoft Graph response limit must be positive');
    const mailbox=config.mailbox?.trim();if(mailbox&&!( /^[0-9A-Fa-f]{8}(?:-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}$/.test(mailbox)||/^[^\s<>@,;]+@[^\s<>@,;]+$/.test(mailbox)))throw new TypeError('configuration mailbox must be an address such as support@contoso.com or an Entra object ID');
    const now=options.now??(()=>new Date());const timeoutMs=options.requestTimeoutMs??20000;if(!Number.isSafeInteger(timeoutMs)||timeoutMs<1)throw new TypeError('Microsoft Graph request timeout must be positive');
    this.#dependencies={config:{...config,maxResponseBytes,mailbox},source:credentials,driver:options.refreshDriver??newGraphRefreshDriver(now),transport:options.transport??sendProviderRequest,now,timeoutMs};
  }
  /** Bind the searchMessages query. */searchMessages(){return new SearchMessagesOperation(this.#dependencies);}
  /** Bind the getMessage query. */getMessage(){return new GetMessageOperation(this.#dependencies);}
  /** Bind the sendMessage mutation. */sendMessage(){return new SendMessageOperation(this.#dependencies);}
  /** Bind the replyToMessage mutation. */replyToMessage(){return new ReplyToMessageOperation(this.#dependencies);}
  /** Bind the moveMessage mutation. */moveMessage(){return new MoveMessageOperation(this.#dependencies);}
  /** Bind the setMessageFlags mutation. */setMessageFlags(){return new SetMessageFlagsOperation(this.#dependencies);}
}
