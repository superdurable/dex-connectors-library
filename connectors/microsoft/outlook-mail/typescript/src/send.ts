// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends,newMutationBranch,newMutationRetry,newMutationUncertain,zeroReceipt,type Call,type Mutation,type MutationAttempt,type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ReplyToMessageBranch,ReplyToMessageDefinition,SendMessageBranch,SendMessageDefinition } from './generated/definitions.js';
import type { ReplyToMessageInput,SendMessageInput,SentMessage } from './generated/models.js';
import { GraphSession,definiteRefusalStatuses,escapePathSegment,graphFailure,isGraphExchange,zeroExchange,type GraphExchange } from './http.js';
import { buildGraphRecipients,graphTimestampUTC,parseGraphMessage,parseJSON,recipientAddresses,validateAddress,validateGraphID,validateSingleLine,type GraphMessageWire } from './message.js';
import type { ClientDependencies } from './operations.js';

export const IdempotencyMarkerPropertyID = 'String {194d4363-ceb1-4554-8c26-1077f4961682} Name DexStepIdempotencyKey';
const draftProperties=['id','isDraft','subject','toRecipients','ccRecipients','bccRecipients','internetMessageId','conversationId','parentFolderId','createdDateTime','sentDateTime'];
/**
 * Connector-owned send checkpoint. `outlookMailSendConfirmations` is written only by releases before
 * 0.1.1 and is ignored. `outlookMailSendCount` counts possibly effective sends: a dispatched checkpoint
 * includes its own send, and a draftReady checkpoint only earlier ones.
 */
interface Checkpoint { outlookMailSendPhase:'draftReady'|'sendDispatched'; outlookMailDraftMessageId:string; outlookMailSendConfirmations?:number; outlookMailSendCount?:number }
const sendConfirmationDelayMs = 3000;
/** Bound the sends a Step execution may have made; a legacy dispatch counts as one. */
function priorSendCount(checkpoint: Checkpoint | undefined): number {
  if (!checkpoint) return 0;
  const sendCount = checkpoint.outlookMailSendCount ?? 0;
  return checkpoint.outlookMailSendPhase === 'sendDispatched' ? Math.max(sendCount, 1) : Math.max(sendCount, 0);
}
function draftCheckpoint(phase: Checkpoint['outlookMailSendPhase'], id: string, sendCount: number): Checkpoint {
  return { outlookMailSendPhase: phase, outlookMailDraftMessageId: id, ...(sendCount ? { outlookMailSendCount: sendCount } : {}) };
}
/** A 429, a 503, or a connection that never reached Graph. */
function isUnprocessedSendAnswer(result: GraphExchange): boolean {
  if (result.status === 0) return result.outcome === 'retry';
  return result.status === 429 || result.status === 503;
}
/** A definite refusal status, or a local request defect before sending. */
function isDefiniteSendRefusal(result: GraphExchange): boolean {
  if (result.status === 0) return result.outcome === 'defect';
  return definiteRefusalStatuses.has(result.status);
}
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}
interface Branches { Sent:string; NotFound?:string; ProviderRejected:string; Defect:string }
interface DraftCreator { create(session:GraphSession,marker:string):Promise<{message?:GraphMessageWire;result:GraphExchange}>; markedOnCreation:boolean }
function emptySent():SentMessage{return {idempotencyMarker:''};}
function sentMessage(message:GraphMessageWire,marker:string):SentMessage{return {...(message.id?{messageId:message.id}:{}),...(message.internetMessageId?{internetMessageId:message.internetMessageId}:{}),...(message.conversationId?{conversationId:message.conversationId}:{}),...(message.subject?{subject:message.subject}:{}),...(recipientAddresses(message).length?{recipients:recipientAddresses(message)}:{}),idempotencyMarker:marker};}
function invalidDraft(result:GraphExchange,operation:string,reason:string):GraphExchange{return {...result,outcome:'invalid',failure:graphFailure(operation,'PROTOCOL','Microsoft Graph returned an invalid answer: '+reason)};}
/** Map a failure before this attempt dispatched a send; a terminal answer is uncertain after an earlier possible send. */
function beforeDispatch(result:GraphExchange,value:SentMessage,receipt:ReceiptDraft,branches:Branches,earlierSendCount:number):MutationAttempt<SentMessage>{if(['retry','ambiguous','invalid'].includes(result.outcome))return newMutationRetry(result.failure!,result.retryAfterMs);
  if(earlierSendCount>0)return newMutationUncertain(value,{...result.failure!,message:result.failure!.message+'; an earlier send may have delivered the message, and it cannot be confirmed or sent again'},receipt);
  if(result.outcome==='notFound')return newMutationBranch(branches.NotFound??branches.ProviderRejected,value,receipt,result.failure);if(result.outcome==='defect')return newMutationBranch(branches.Defect,value,receipt,result.failure);return newMutationBranch(branches.ProviderRejected,value,receipt,{...result.failure!,message:result.failure!.message+'; nothing was sent'});}
async function readDraft(session:GraphSession,id:string):Promise<{message?:GraphMessageWire;result:GraphExchange}>{const result=await session.exchange({method:'GET',path:'/messages/'+escapePathSegment(id),query:[['$select',draftProperties.join(',')]]});if(result.outcome!=='succeeded')return {result};try{return {message:parseGraphMessage(parseJSON(result.body)),result};}catch(error){return {result:invalidDraft(result,session.operation,'the draft read returned '+(error as Error).message)};}}
async function findDraft(session:GraphSession,marker:string):Promise<{message?:GraphMessageWire;result:GraphExchange}>{const filter=`singleValueExtendedProperties/Any(ep: ep/id eq '${IdempotencyMarkerPropertyID.replace(/'/g,"''")}' and ep/value eq '${marker.replace(/'/g,"''")}')`;const result=await session.exchange({method:'GET',path:'/messages',query:[['$filter',filter],['$select',draftProperties.join(',')],['$top','10']]});if(result.outcome!=='succeeded')return {result};try{const document=parseJSON(result.body) as Record<string,unknown>;if(!Array.isArray(document.value))throw new TypeError('the draft lookup has no value list');const matches=document.value.slice(0,10).map(parseGraphMessage);matches.sort((left,right)=>{if(!!left.isDraft!==!!right.isDraft)return left.isDraft?1:-1;if(!left.createdDateTime&&!right.createdDateTime)return 0;if(!left.createdDateTime)return 1;if(!right.createdDateTime)return -1;return compareDraftTime(left.createdDateTime,right.createdDateTime)||left.id.localeCompare(right.id);});return {message:matches[0],result};}catch(error){return {result:invalidDraft(result,session.operation,'the draft lookup returned '+(error as Error).message)};}}
function compareDraftTime(left: string, right: string): number {
  const order = (value: string): string => {
    const utc = graphTimestampUTC(value);
    const [whole, fraction = ''] = utc.slice(0, -1).split('.');
    return whole! + fraction.padEnd(9, '0');
  };
  const first = order(left), second = order(right);
  return first < second ? -1 : first > second ? 1 : 0;
}
class DraftSender {
  constructor(private readonly call:Call,private readonly session:GraphSession,private readonly marker:string,private readonly creator:DraftCreator,
    private readonly branches:Branches,private readonly earlierSendCount:number,private readonly maxSends:number){}
  async run(checkpoint?:Checkpoint):Promise<MutationAttempt<SentMessage>>{
    if(checkpoint?.outlookMailSendPhase==='sendDispatched')return this.confirmDispatched(checkpoint);if(checkpoint?.outlookMailSendPhase==='draftReady')return this.resumeDraft(checkpoint.outlookMailDraftMessageId);return this.findOrCreate();}
  /** Read a dispatched draft back; send a still-unsent draft again while the send cap allows. */
  private async confirmDispatched(checkpoint:Checkpoint):Promise<MutationAttempt<SentMessage>>{
    const pending:SentMessage={messageId:checkpoint.outlookMailDraftMessageId,idempotencyMarker:this.marker};
    // The marker lookup reads the message back without the draft ID.
    try{validateGraphID(checkpoint.outlookMailDraftMessageId,'draft message id');}catch{return this.findOrCreate();}
    const {message,result}=await readDraft(this.session,checkpoint.outlookMailDraftMessageId);const receipt=this.session.receipt(result,checkpoint.outlookMailDraftMessageId);
    if(result.outcome==='succeeded'&&message){
      if(!message.isDraft)return newMutationBranch(this.branches.Sent,{...sentMessage(message,this.marker),wasAlreadySent:true},receipt);
      return this.dispatch(message);
    }
    if(result.outcome==='notFound')return this.findOrCreate();
    if(['retry','ambiguous','invalid'].includes(result.outcome))return newMutationRetry(result.failure!,result.retryAfterMs);
    return newMutationUncertain(pending,{...result.failure!,message:result.failure!.message+'; the dispatched draft could not be read back, so the message may have been sent; it is not sent again'},receipt);
  }
  private async resumeDraft(id:string):Promise<MutationAttempt<SentMessage>>{try{validateGraphID(id,'draft message id');}catch{return this.findOrCreate();}const {message,result}=await readDraft(this.session,id);if(result.outcome==='notFound')return this.findOrCreate();if(result.outcome!=='succeeded'||!message)return beforeDispatch(result,{messageId:id,idempotencyMarker:this.marker},this.session.receipt(result,id),this.branches,this.earlierSendCount);if(!message.isDraft)return newMutationBranch(this.branches.Sent,{...sentMessage(message,this.marker),wasAlreadySent:true},this.session.receipt(result,message.id));return this.dispatch(message);}
  private async findOrCreate():Promise<MutationAttempt<SentMessage>>{const found=await findDraft(this.session,this.marker);if(found.result.outcome!=='succeeded')return beforeDispatch(found.result,{idempotencyMarker:this.marker},this.session.receipt(found.result),this.branches,this.earlierSendCount);if(found.message){if(!found.message.isDraft)return newMutationBranch(this.branches.Sent,{...sentMessage(found.message,this.marker),wasAlreadySent:true},this.session.receipt(found.result,found.message.id));return this.dispatch(found.message);}
    const created=await this.creator.create(this.session,this.marker);if(created.result.outcome!=='succeeded'||!created.message)return beforeDispatch(created.result,{idempotencyMarker:this.marker},this.session.receipt(created.result),this.branches,this.earlierSendCount);const draft=created.message;try{await this.call.context.recordHeartbeat(draftCheckpoint('draftReady',draft.id,this.earlierSendCount));}catch{/* Marker lookup can recover this draft. */}
    if(!this.creator.markedOnCreation){const marked=await this.session.exchange({method:'PATCH',path:'/messages/'+escapePathSegment(draft.id),payload:{singleValueExtendedProperties:[{id:IdempotencyMarkerPropertyID,value:this.marker}]}});if(marked.outcome==='notFound')return newMutationRetry(marked.failure!);if(marked.outcome!=='succeeded')return beforeDispatch(marked,sentMessage(draft,this.marker),this.session.receipt(marked,draft.id),this.branches,this.earlierSendCount);}
    return this.dispatch(draft);
  }
  /** Record the dispatch checkpoint, then send while the cap allows; only an unprocessed answer releases it. */
  private async dispatch(draft:GraphMessageWire):Promise<MutationAttempt<SentMessage>>{
    const operation=this.session.operation;const message=sentMessage(draft,this.marker);
    if(this.earlierSendCount>=this.maxSends)return newMutationUncertain(message,graphFailure(operation,'TRANSPORT',`an earlier send of the draft was not confirmed and the send cap of ${this.maxSends} is reached, so the message may have been sent; it is not sent again`),this.session.receipt(zeroExchange('succeeded',graphFailure(operation,'TRANSPORT','')),draft.id));
    try{await this.call.context.recordHeartbeat(draftCheckpoint('sendDispatched',draft.id,this.earlierSendCount+1));}catch{return newMutationRetry(graphFailure(operation,'AVAILABILITY','Dex did not record the send checkpoint; nothing was sent'));}
    const result=await this.session.exchange({method:'POST',path:'/messages/'+escapePathSegment(draft.id)+'/send'});const receipt=this.session.receipt(result,draft.id);
    // Any 2xx means Graph accepted the send; the send action returns no body to validate.
    if(result.outcome==='succeeded'||result.outcome==='invalid')return newMutationBranch(this.branches.Sent,message,receipt);
    if(isUnprocessedSendAnswer(result)){await this.release(draft.id);return newMutationRetry(result.failure!,result.retryAfterMs);}
    if(isDefiniteSendRefusal(result)&&this.earlierSendCount===0){await this.release(draft.id);return newMutationBranch(this.branches.ProviderRejected,message,receipt,{...result.failure!,message:result.failure!.message+'; nothing was sent and the draft stays in Drafts'});}
    // An earlier send may have delivered the draft, so the next attempt reads it back.
    if(isDefiniteSendRefusal(result))return newMutationRetry({...result.failure!,message:result.failure!.message+'; an earlier send may have delivered the draft, so a later attempt reads it back'},sendConfirmationDelayMs);
    return newMutationRetry({...result.failure!,message:result.failure!.message+'; the send was dispatched, so a later attempt reads the draft back before sending again'},sendConfirmationDelayMs);
  }
  private async release(id:string):Promise<void>{try{await this.call.context.recordHeartbeat(draftCheckpoint('draftReady',id,this.earlierSendCount));}catch{/* An unreleased dispatch checkpoint safely prevents another send. */}}
}
function validateSendText(text:string):void{if(!text?.trim())throw new TypeError('text is required');if(Buffer.byteLength(text)>524288)throw new TypeError('text can be at most 524288 bytes');}
async function sendAttempt(call:Call,dependencies:ClientDependencies,operation:string,creator:DraftCreator,branches:Branches,operationMaxSends:number):Promise<MutationAttempt<SentMessage>>{const key=call.idempotencyKey??'';if(!/^[A-Za-z0-9._:-]{1,128}$/.test(key))return newMutationBranch(branches.Defect,emptySent(),zeroReceipt(),graphFailure(operation,'LOCAL_DEFECT','the Step has no usable idempotency key for the draft marker'));const marker='dex-'+key;
  let checkpoint:Checkpoint|undefined;
  try { if (call.context.hasLastHeartbeatValue()) {
    const value=call.context.getLastHeartbeatValue<Checkpoint>();
    if(!value||!['draftReady','sendDispatched'].includes(value.outlookMailSendPhase))throw new TypeError('invalid send checkpoint');
    if(value.outlookMailSendCount!==undefined&&!(Number.isSafeInteger(value.outlookMailSendCount)&&value.outlookMailSendCount>=0))throw new TypeError('invalid send checkpoint');
    checkpoint=value;
  } } catch { return newMutationRetry(graphFailure(operation,'AVAILABILITY','the send checkpoint of an earlier attempt is unreadable; nothing is sent in this attempt')); }
  const earlierSendCount=priorSendCount(checkpoint);
  const opened=await GraphSession.open(call,operation,dependencies.config,dependencies.source,dependencies.driver,dependencies.transport,dependencies.now,dependencies.timeoutMs);if(isGraphExchange(opened))return beforeDispatch(opened,{idempotencyMarker:marker},zeroReceipt(),branches,earlierSendCount);
  return new DraftSender(call,opened,marker,creator,branches,earlierSendCount,effectiveMaxSends(call,operationMaxSends)).run(checkpoint);}
/** Create and send a marked message draft. */
export class SendMessageOperation implements Mutation<SendMessageInput,SentMessage>{constructor(private readonly dependencies:ClientDependencies){}definition(){return SendMessageDefinition;}idempotencyKey(callId:string){return callId;}
  async invoke(call:Call,input:SendMessageInput):Promise<MutationAttempt<SentMessage>>{try{if(!input.to?.length)throw new TypeError('to needs at least one recipient');if(input.to.length+(input.cc?.length??0)+(input.bcc?.length??0)>500)throw new TypeError('to, cc, and bcc can hold at most 500 recipients together');for(const [field,values] of [['to',input.to],['cc',input.cc],['bcc',input.bcc],['replyTo',input.replyTo]] as const)for(const address of values??[])validateAddress(address,field);if(!input.subject?.trim())throw new TypeError('subject is required');validateSingleLine(input.subject,'subject',255);validateSendText(input.text);}catch(error){return newMutationBranch(SendMessageBranch.Defect,emptySent(),zeroReceipt(),graphFailure('sendMessage','VALIDATION',(error as Error).message));}
    const creator:DraftCreator={markedOnCreation:true,create:async(session,marker)=>{const payload:Record<string,unknown>={subject:input.subject,body:{contentType:'text',content:input.text},toRecipients:buildGraphRecipients(input.to ?? []),singleValueExtendedProperties:[{id:IdempotencyMarkerPropertyID,value:marker}]};if(input.cc?.length)payload.ccRecipients=buildGraphRecipients(input.cc);if(input.bcc?.length)payload.bccRecipients=buildGraphRecipients(input.bcc);if(input.replyTo?.length)payload.replyTo=buildGraphRecipients(input.replyTo ?? []);let result=await session.exchange({method:'POST',path:'/messages',payload});if(result.outcome!=='succeeded')return {result};try{return {message:parseGraphMessage(parseJSON(result.body)),result};}catch(error){result=invalidDraft(result,session.operation,'the created draft '+(error as Error).message);return {result};}}};return sendAttempt(call,this.dependencies,'sendMessage',creator,SendMessageBranch,definitionMaxSends(SendMessageDefinition));
  }}
/** Create Outlook's threaded reply and send it through a marked draft. */
export class ReplyToMessageOperation implements Mutation<ReplyToMessageInput,SentMessage>{constructor(private readonly dependencies:ClientDependencies){}definition(){return ReplyToMessageDefinition;}idempotencyKey(callId:string){return callId;}
  async invoke(call:Call,input:ReplyToMessageInput):Promise<MutationAttempt<SentMessage>>{const id=input.messageId?.trim();try{validateGraphID(id);validateSendText(input.text);}catch(error){return newMutationBranch(ReplyToMessageBranch.Defect,emptySent(),zeroReceipt(),graphFailure('replyToMessage','VALIDATION',(error as Error).message));}
    const creator:DraftCreator={markedOnCreation:false,create:async(session)=>{const action=input.isReplyAll?'/createReplyAll':'/createReply';let result=await session.exchange({method:'POST',path:'/messages/'+escapePathSegment(id)+action,payload:{comment:input.text}});if(result.outcome==='invalid')return {result:{...result,outcome:'rejected',failure:graphFailure('replyToMessage','PROTOCOL','Microsoft Graph created a reply draft but its answer is unusable; the unsent draft stays in Drafts')}};if(result.outcome!=='succeeded')return {result};try{const message=parseGraphMessage(parseJSON(result.body));if(!recipientAddresses(message).length)throw new TypeError('the original has no address to reply to');return {message,result};}catch(error){result={...result,outcome:'rejected',failure:graphFailure('replyToMessage','PROTOCOL','Microsoft Graph created a reply draft but '+(error as Error).message+'; the unsent draft stays in Drafts')};return {result};}}};return sendAttempt(call,this.dependencies,'replyToMessage',creator,ReplyToMessageBranch,definitionMaxSends(ReplyToMessageDefinition));
  }}
