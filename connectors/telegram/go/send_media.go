// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SendMediaInput sends a Telegram file ID or public HTTPS media URL.
type SendMediaInput struct {
	// ChatID is a decimal chat ID or public @username.
	ChatID string `json:"chatId"`
	// Media is a Telegram file ID or public HTTPS URL without credentials.
	Media string `json:"media"`
	// Caption is optional UTF-8 text up to 1024 characters.
	Caption string `json:"caption,omitempty"`
}

// SendAnimationInput sends one animation.
type SendAnimationInput = SendMediaInput

// SendAudioInput sends one audio file.
type SendAudioInput = SendMediaInput

// SendDocumentInput sends one document.
type SendDocumentInput = SendMediaInput

// SendPhotoInput sends one photo.
type SendPhotoInput = SendMediaInput

// SendVideoInput sends one video.
type SendVideoInput = SendMediaInput

// SendStickerInput sends one sticker without a caption.
type SendStickerInput struct {
	// ChatID is a decimal chat ID or public @username.
	ChatID string `json:"chatId"`
	// Media is a sticker file ID or public HTTPS URL.
	Media string `json:"media"`
}

// SendLocationInput sends one geographic point.
type SendLocationInput struct {
	// ChatID is a decimal chat ID or public @username.
	ChatID string `json:"chatId"`
	// Latitude is degrees from -90 through 90.
	Latitude float64 `json:"latitude"`
	// Longitude is degrees from -180 through 180.
	Longitude float64 `json:"longitude"`
}

// MediaGroupItem names one photo or video in a Telegram album.
type MediaGroupItem struct {
	// Type is photo or video.
	Type string `json:"type"`
	// Media is a Telegram file ID or public HTTPS URL.
	Media string `json:"media"`
	// Caption is optional UTF-8 text up to 1024 characters.
	Caption string `json:"caption,omitempty"`
}

// SendMediaGroupInput sends an album of two through ten photos or videos.
type SendMediaGroupInput struct {
	// ChatID is a decimal chat ID or public @username.
	ChatID string `json:"chatId"`
	// Media is the ordered album content.
	Media []MediaGroupItem `json:"media"`
}

// SentMessages contains confirmed Telegram messages from one album send.
type SentMessages struct {
	// Messages are the provider-confirmed message identities in album order.
	Messages []SentMessage `json:"messages"`
}

type mediaHTTPResponse struct {
	status           int
	body             []byte
	retryAfter       time.Duration
	err              error
	didGetConnection bool
}

type mediaDispatch struct {
	client   *Client
	token    string
	method   string
	form     url.Values
	response mediaHTTPResponse
}

func (dispatch *mediaDispatch) send(requestContext context.Context) error {
	endpoint := dispatch.client.baseURL + "/bot" + dispatch.token + "/" + dispatch.method
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, endpoint, strings.NewReader(dispatch.form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return err
	}
	var connected atomic.Bool
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected.Store(true) }}
	response, err := dispatch.client.httpClient.Do(request.WithContext(httptrace.WithClientTrace(requestContext, trace)))
	dispatch.response.didGetConnection = connected.Load()
	if err != nil {
		dispatch.response.err = err
		return nil
	}
	defer func() { _ = response.Body.Close() }()
	dispatch.response.status = response.StatusCode
	dispatch.response.retryAfter = providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		dispatch.response.retryAfter = retryAfterFromResponse(response, dispatch.client.maxResponseBytes)
		return nil
	}
	dispatch.response.body, dispatch.response.err = providerhttp.ReadBoundedBody(response.Body, dispatch.client.maxResponseBytes)
	return nil
}

func invokeMediaSend[T any](call sdkgo.Call, client *Client, method string, form url.Values, timeout time.Duration, sent, rejected, defect sdkgo.BranchID, parseResult func(json.RawMessage) (T, string, error)) sdkgo.MutationAttempt[T] {
	failure := func(kind sdkgo.FailureKind, message string) sdkgo.Failure {
		return sdkgo.Failure{Kind: kind, Provider: "telegram", Operation: method, Message: message}
	}
	branch := func(branchID sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
		var zero T
		problem := failure(kind, message)
		return sdkgo.NewMutationBranch(branchID, zero, &problem, sdkgo.Receipt{})
	}
	uncertain := func(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
		var zero T
		return sdkgo.NewMutationUncertain(zero, failure(kind, message), sdkgo.Receipt{})
	}
	retry := func(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[T] {
		return sdkgo.NewMutationRetry[T](failure(kind, message), delay)
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return branch(defect, sdkgo.FailureAuthentication, "bot token unavailable")
	}
	token := credentials.BotToken.Reveal()
	if !botTokenPattern.MatchString(token) {
		return branch(defect, sdkgo.FailureAuthentication, "bot token invalid")
	}
	fence, err := singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return branch(defect, sdkgo.FailureLocalDefect, "Telegram dispatch checkpoint unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return branch(defect, sdkgo.FailureLocalDefect, "Telegram dispatch checkpoint invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uncertain(sdkgo.FailureAvailability, "Telegram send could not be confirmed")
	}
	request := &mediaDispatch{client: client, token: token, method: method, form: form}
	if err := fence.Dispatch(request.send); err != nil {
		return retry(sdkgo.FailureTransport, "Telegram dispatch was not started", 0)
	}
	response := request.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return retry(sdkgo.FailureAvailability, "retry checkpoint unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return retry(kind, "Telegram did not process the send", response.retryAfter)
	}
	if response.err != nil && !response.didGetConnection {
		if fence.RecordCanResend() == nil {
			return retry(sdkgo.FailureTransport, "Telegram was unreachable before send", 0)
		}
	}
	retryUnknown := func(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return uncertain(kind, message)
		}
		return retry(kind, message, response.retryAfter)
	}
	if response.err != nil {
		return retryUnknown(sdkgo.FailureTransport, "Telegram send outcome unknown")
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return retryUnknown(sdkgo.FailureAvailability, "Telegram send outcome unknown")
	}
	if response.status >= 400 {
		return branch(rejected, sdkgo.FailureProviderRejection, "Telegram rejected HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		OK        bool            `json:"ok"`
		Result    json.RawMessage `json:"result"`
		ErrorCode int             `json:"error_code"`
	}
	if json.Unmarshal(response.body, &envelope) != nil {
		return retryUnknown(sdkgo.FailureProtocol, "Telegram send response invalid")
	}
	if !envelope.OK {
		if envelope.ErrorCode == http.StatusTooManyRequests || envelope.ErrorCode == http.StatusServiceUnavailable {
			if fence.RecordCanResend() != nil {
				return retry(sdkgo.FailureAvailability, "retry checkpoint unavailable", 0)
			}
			return retry(sdkgo.FailureRateLimit, "Telegram did not process the send", response.retryAfter)
		}
		if envelope.ErrorCode >= 500 {
			return retryUnknown(sdkgo.FailureAvailability, "Telegram send outcome unknown")
		}
		if envelope.ErrorCode >= 400 {
			return branch(rejected, sdkgo.FailureProviderRejection, "Telegram rejected the send")
		}
		return retryUnknown(sdkgo.FailureProtocol, "Telegram send response invalid")
	}
	if response.status != http.StatusOK {
		return retryUnknown(sdkgo.FailureProtocol, "Telegram send status invalid")
	}
	result, providerID, err := parseResult(envelope.Result)
	if err != nil {
		return retryUnknown(sdkgo.FailureProtocol, "Telegram message identity invalid")
	}
	if err := fence.RecordProviderID(providerID); err != nil {
		return branch(defect, sdkgo.FailureLocalDefect, "Telegram confirmation checkpoint unavailable")
	}
	return sdkgo.NewMutationBranch(sent, result, nil, sdkgo.Receipt{})
}

func parseSentMessage(raw json.RawMessage) (SentMessage, string, error) {
	var wire struct {
		MessageID int64 `json:"message_id"`
		Date      int64 `json:"date"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	}
	if json.Unmarshal(raw, &wire) != nil || wire.MessageID <= 0 || wire.Date <= 0 || wire.Chat.ID == 0 {
		return SentMessage{}, "", fmt.Errorf("invalid Telegram message identity")
	}
	return SentMessage{ChatID: wire.Chat.ID, MessageID: wire.MessageID, Date: wire.Date}, strconv.FormatInt(wire.MessageID, 10), nil
}

func parseSentMessages(raw json.RawMessage) (SentMessages, string, error) {
	var messages []json.RawMessage
	if json.Unmarshal(raw, &messages) != nil || len(messages) < 2 || len(messages) > 10 {
		return SentMessages{}, "", fmt.Errorf("invalid Telegram album")
	}
	result := SentMessages{Messages: make([]SentMessage, 0, len(messages))}
	providerID := ""
	for _, message := range messages {
		parsed, identifier, err := parseSentMessage(message)
		if err != nil {
			return SentMessages{}, "", err
		}
		if providerID == "" {
			providerID = identifier
		}
		result.Messages = append(result.Messages, parsed)
	}
	return result, providerID, nil
}

func validMediaReference(media string) bool {
	if len(media) == 0 || len(media) > 1024 || strings.TrimSpace(media) != media {
		return false
	}
	if strings.HasPrefix(media, "https://") {
		parsed, err := url.Parse(media)
		return err == nil && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
	}
	if strings.Contains(media, "://") {
		return false
	}
	return !strings.ContainsAny(media, " \t\n\r")
}

func validMediaCaption(caption string) bool {
	return utf8.ValidString(caption) && utf8.RuneCountInString(caption) <= 1024
}

func validLocation(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsInf(latitude, 0) && !math.IsNaN(longitude) && !math.IsInf(longitude, 0) && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}
