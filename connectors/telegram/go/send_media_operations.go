// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func invalidMediaInput[T any](operation string) sdkgo.MutationAttempt[T] {
	var zero T
	problem := sdkgo.Failure{Kind: sdkgo.FailureValidation, Provider: "telegram", Operation: operation, Message: "Telegram send input is invalid"}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("defect"), zero, &problem, sdkgo.Receipt{})
}

// SendAnimationOperation sends one animation with a bounded resend cap.
type SendAnimationOperation struct{ client *Client }

// SendAnimation returns the typed animation mutation.
func (client *Client) SendAnimation() SendAnimationOperation { return SendAnimationOperation{client} }

// Definition returns the generated animation contract.
func (SendAnimationOperation) Definition() sdkgo.MutationDefinition { return SendAnimationDefinition }

// IdempotencyKey records the call identity; Telegram does not deduplicate it.
func (SendAnimationOperation) IdempotencyKey(callID sdkgo.CallID, _ SendAnimationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke dispatches the animation to Telegram.
func (operation SendAnimationOperation) Invoke(call sdkgo.Call, input SendAnimationInput) sdkgo.MutationAttempt[SentMessage] {
	if !validChatID(input.ChatID) || !validMediaReference(input.Media) || !validMediaCaption(input.Caption) {
		return invalidMediaInput[SentMessage]("sendAnimation")
	}
	form := url.Values{"chat_id": {input.ChatID}, "animation": {input.Media}}
	if input.Caption != "" {
		form.Set("caption", input.Caption)
	}
	return invokeMediaSend(call, operation.client, "sendAnimation", form, SendAnimationDefinition.StepDefaults.ExecuteMethodTimeout, SendAnimationBranchSent, SendAnimationBranchProviderRejected, SendAnimationBranchDefect, parseSentMessage)
}

// SendAudioOperation sends one audio with a bounded resend cap.
type SendAudioOperation struct{ client *Client }

// SendAudio returns the typed audio mutation.
func (client *Client) SendAudio() SendAudioOperation { return SendAudioOperation{client} }

// Definition returns the generated audio contract.
func (SendAudioOperation) Definition() sdkgo.MutationDefinition { return SendAudioDefinition }

// IdempotencyKey records the call identity; Telegram does not deduplicate it.
func (SendAudioOperation) IdempotencyKey(callID sdkgo.CallID, _ SendAudioInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke dispatches the audio to Telegram.
func (operation SendAudioOperation) Invoke(call sdkgo.Call, input SendAudioInput) sdkgo.MutationAttempt[SentMessage] {
	if !validChatID(input.ChatID) || !validMediaReference(input.Media) || !validMediaCaption(input.Caption) {
		return invalidMediaInput[SentMessage]("sendAudio")
	}
	form := url.Values{"chat_id": {input.ChatID}, "audio": {input.Media}}
	if input.Caption != "" {
		form.Set("caption", input.Caption)
	}
	return invokeMediaSend(call, operation.client, "sendAudio", form, SendAudioDefinition.StepDefaults.ExecuteMethodTimeout, SendAudioBranchSent, SendAudioBranchProviderRejected, SendAudioBranchDefect, parseSentMessage)
}

// SendDocumentOperation sends one document with a bounded resend cap.
type SendDocumentOperation struct{ client *Client }

// SendDocument returns the typed document mutation.
func (client *Client) SendDocument() SendDocumentOperation { return SendDocumentOperation{client} }

// Definition returns the generated document contract.
func (SendDocumentOperation) Definition() sdkgo.MutationDefinition { return SendDocumentDefinition }

// IdempotencyKey records the call identity; Telegram does not deduplicate it.
func (SendDocumentOperation) IdempotencyKey(callID sdkgo.CallID, _ SendDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke dispatches the document to Telegram.
func (operation SendDocumentOperation) Invoke(call sdkgo.Call, input SendDocumentInput) sdkgo.MutationAttempt[SentMessage] {
	if !validChatID(input.ChatID) || !validMediaReference(input.Media) || !validMediaCaption(input.Caption) {
		return invalidMediaInput[SentMessage]("sendDocument")
	}
	form := url.Values{"chat_id": {input.ChatID}, "document": {input.Media}}
	if input.Caption != "" {
		form.Set("caption", input.Caption)
	}
	return invokeMediaSend(call, operation.client, "sendDocument", form, SendDocumentDefinition.StepDefaults.ExecuteMethodTimeout, SendDocumentBranchSent, SendDocumentBranchProviderRejected, SendDocumentBranchDefect, parseSentMessage)
}

// SendPhotoOperation sends one photo with a bounded resend cap.
type SendPhotoOperation struct{ client *Client }

// SendPhoto returns the typed photo mutation.
func (client *Client) SendPhoto() SendPhotoOperation { return SendPhotoOperation{client} }

// Definition returns the generated photo contract.
func (SendPhotoOperation) Definition() sdkgo.MutationDefinition { return SendPhotoDefinition }

// IdempotencyKey records the call identity; Telegram does not deduplicate it.
func (SendPhotoOperation) IdempotencyKey(callID sdkgo.CallID, _ SendPhotoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke dispatches the photo to Telegram.
func (operation SendPhotoOperation) Invoke(call sdkgo.Call, input SendPhotoInput) sdkgo.MutationAttempt[SentMessage] {
	if !validChatID(input.ChatID) || !validMediaReference(input.Media) || !validMediaCaption(input.Caption) {
		return invalidMediaInput[SentMessage]("sendPhoto")
	}
	form := url.Values{"chat_id": {input.ChatID}, "photo": {input.Media}}
	if input.Caption != "" {
		form.Set("caption", input.Caption)
	}
	return invokeMediaSend(call, operation.client, "sendPhoto", form, SendPhotoDefinition.StepDefaults.ExecuteMethodTimeout, SendPhotoBranchSent, SendPhotoBranchProviderRejected, SendPhotoBranchDefect, parseSentMessage)
}

// SendVideoOperation sends one video with a bounded resend cap.
type SendVideoOperation struct{ client *Client }

// SendVideo returns the typed video mutation.
func (client *Client) SendVideo() SendVideoOperation { return SendVideoOperation{client} }

// Definition returns the generated video contract.
func (SendVideoOperation) Definition() sdkgo.MutationDefinition { return SendVideoDefinition }

// IdempotencyKey records the call identity; Telegram does not deduplicate it.
func (SendVideoOperation) IdempotencyKey(callID sdkgo.CallID, _ SendVideoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke dispatches the video to Telegram.
func (operation SendVideoOperation) Invoke(call sdkgo.Call, input SendVideoInput) sdkgo.MutationAttempt[SentMessage] {
	if !validChatID(input.ChatID) || !validMediaReference(input.Media) || !validMediaCaption(input.Caption) {
		return invalidMediaInput[SentMessage]("sendVideo")
	}
	form := url.Values{"chat_id": {input.ChatID}, "video": {input.Media}}
	if input.Caption != "" {
		form.Set("caption", input.Caption)
	}
	return invokeMediaSend(call, operation.client, "sendVideo", form, SendVideoDefinition.StepDefaults.ExecuteMethodTimeout, SendVideoBranchSent, SendVideoBranchProviderRejected, SendVideoBranchDefect, parseSentMessage)
}

// SendStickerOperation sends one sticker with a bounded resend cap.
type SendStickerOperation struct{ client *Client }

// SendSticker returns the typed sticker mutation.
func (client *Client) SendSticker() SendStickerOperation { return SendStickerOperation{client} }

// Definition returns the generated sticker contract.
func (SendStickerOperation) Definition() sdkgo.MutationDefinition { return SendStickerDefinition }

// IdempotencyKey records the call identity; Telegram does not deduplicate it.
func (SendStickerOperation) IdempotencyKey(callID sdkgo.CallID, _ SendStickerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke dispatches the sticker to Telegram.
func (operation SendStickerOperation) Invoke(call sdkgo.Call, input SendStickerInput) sdkgo.MutationAttempt[SentMessage] {
	if !validChatID(input.ChatID) || !validMediaReference(input.Media) {
		return invalidMediaInput[SentMessage]("sendSticker")
	}
	form := url.Values{"chat_id": {input.ChatID}, "sticker": {input.Media}}
	return invokeMediaSend(call, operation.client, "sendSticker", form, SendStickerDefinition.StepDefaults.ExecuteMethodTimeout, SendStickerBranchSent, SendStickerBranchProviderRejected, SendStickerBranchDefect, parseSentMessage)
}

// SendLocationOperation sends one geographic point with a bounded resend cap.
type SendLocationOperation struct{ client *Client }

// SendLocation returns the typed location mutation.
func (client *Client) SendLocation() SendLocationOperation { return SendLocationOperation{client} }

// Definition returns the generated location contract.
func (SendLocationOperation) Definition() sdkgo.MutationDefinition { return SendLocationDefinition }

// IdempotencyKey records the call identity; Telegram does not deduplicate it.
func (SendLocationOperation) IdempotencyKey(callID sdkgo.CallID, _ SendLocationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke dispatches the location to Telegram.
func (operation SendLocationOperation) Invoke(call sdkgo.Call, input SendLocationInput) sdkgo.MutationAttempt[SentMessage] {
	if !validChatID(input.ChatID) || !validLocation(input.Latitude, input.Longitude) {
		return invalidMediaInput[SentMessage]("sendLocation")
	}
	form := url.Values{"chat_id": {input.ChatID}, "latitude": {strconv.FormatFloat(input.Latitude, 'f', -1, 64)}, "longitude": {strconv.FormatFloat(input.Longitude, 'f', -1, 64)}}
	return invokeMediaSend(call, operation.client, "sendLocation", form, SendLocationDefinition.StepDefaults.ExecuteMethodTimeout, SendLocationBranchSent, SendLocationBranchProviderRejected, SendLocationBranchDefect, parseSentMessage)
}

// SendMediaGroupOperation sends two through ten media items with a bounded resend cap.
type SendMediaGroupOperation struct{ client *Client }

// SendMediaGroup returns the typed album mutation.
func (client *Client) SendMediaGroup() SendMediaGroupOperation {
	return SendMediaGroupOperation{client}
}

// Definition returns the generated album contract.
func (SendMediaGroupOperation) Definition() sdkgo.MutationDefinition { return SendMediaGroupDefinition }

// IdempotencyKey records the call identity; Telegram does not deduplicate it.
func (SendMediaGroupOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMediaGroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke dispatches the album to Telegram.
func (operation SendMediaGroupOperation) Invoke(call sdkgo.Call, input SendMediaGroupInput) sdkgo.MutationAttempt[SentMessages] {
	if !validChatID(input.ChatID) || len(input.Media) < 2 || len(input.Media) > 10 {
		return invalidMediaInput[SentMessages]("sendMediaGroup")
	}
	for _, item := range input.Media {
		if (item.Type != "photo" && item.Type != "video") || !validMediaReference(item.Media) || !validMediaCaption(item.Caption) {
			return invalidMediaInput[SentMessages]("sendMediaGroup")
		}
	}
	media, err := json.Marshal(input.Media)
	if err != nil {
		return invalidMediaInput[SentMessages]("sendMediaGroup")
	}
	form := url.Values{"chat_id": {input.ChatID}, "media": {string(media)}}
	return invokeMediaSend(call, operation.client, "sendMediaGroup", form, SendMediaGroupDefinition.StepDefaults.ExecuteMethodTimeout, SendMediaGroupBranchSent, SendMediaGroupBranchProviderRejected, SendMediaGroupBranchDefect, parseSentMessages)
}
