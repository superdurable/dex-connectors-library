// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package messaging sends SMS, MMS, and WhatsApp messages through Twilio
// Programmable Messaging and reads their delivery status.
//
// Twilio's Messages API accepts no idempotency key, so a repeated create
// request sends a second text. SendMessage therefore retries only when Twilio
// cannot have created a message: a 429 rejection, or a failure before any
// connection to Twilio opened. A timeout, dropped connection, 408, 5xx, or
// unreadable response after dispatch selects the uncertain branch, and the
// application reconciles it instead of sending again.
package messaging

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "twilio"

	// requestTimeout expires before the 30-second Execute timeout, so a hung send selects uncertain.
	requestTimeout        = 20 * time.Second
	maximumBodyCharacters = 1600
	maximumMediaURLs      = 10
	maximumMediaURLBytes  = 2048
	maximumAddressBytes   = 64
	requestIDHeader       = "Twilio-Request-Id"
	errorCodeMetadataKey  = "twilioErrorCode"
)

var (
	accountSIDPattern          = regexp.MustCompile(`^AC[0-9a-f]{32}$`)
	apiKeySIDPattern           = regexp.MustCompile(`^SK[0-9a-f]{32}$`)
	messagingServiceSIDPattern = regexp.MustCompile(`^MG[0-9a-f]{32}$`)
	messageSIDPattern          = regexp.MustCompile(`^(SM|MM)[0-9a-f]{32}$`)
	phoneNumberPattern         = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	messageStatusPattern       = regexp.MustCompile(`^[a-z_]{1,32}$`)
	messageDirectionPattern    = regexp.MustCompile(`^[a-z-]{1,32}$`)
	pricePattern               = regexp.MustCompile(`^-?[0-9]{1,12}(\.[0-9]{1,12})?$`)
	priceUnitPattern           = regexp.MustCompile(`^[A-Z]{3}$`)
	errorCodePattern           = regexp.MustCompile(`^[0-9]{1,9}$`)
)

var (
	errMessageResponseInvalid = errors.New("Twilio returned an invalid message")
	errRequestNotBuilt        = errors.New("Twilio request could not be built")
)

// MessageStatus is Twilio's own message status value, preserved without mapping.
// Twilio may add values; an unknown value is still returned verbatim.
type MessageStatus string

const (
	// MessageStatusAccepted means a Messaging Service accepted the message and has not chosen a sender yet.
	MessageStatusAccepted MessageStatus = "accepted"
	// MessageStatusScheduled means the message is scheduled for later sending.
	MessageStatusScheduled MessageStatus = "scheduled"
	// MessageStatusQueued means Twilio queued the message for sending.
	MessageStatusQueued MessageStatus = "queued"
	// MessageStatusSending means Twilio is dispatching the message to the carrier or WhatsApp.
	MessageStatusSending MessageStatus = "sending"
	// MessageStatusSent means the carrier or WhatsApp accepted the message. Some destinations never report more.
	MessageStatusSent MessageStatus = "sent"
	// MessageStatusDelivered means the carrier or WhatsApp confirmed delivery to the handset.
	MessageStatusDelivered MessageStatus = "delivered"
	// MessageStatusRead means the WhatsApp recipient read the message.
	MessageStatusRead MessageStatus = "read"
	// MessageStatusUndelivered means the carrier or WhatsApp reported that delivery failed; ErrorCode explains why.
	MessageStatusUndelivered MessageStatus = "undelivered"
	// MessageStatusFailed means Twilio could not send the message; ErrorCode explains why.
	MessageStatusFailed MessageStatus = "failed"
	// MessageStatusCanceled means a scheduled message was canceled before sending.
	MessageStatusCanceled MessageStatus = "canceled"
)

// Message is the connector-safe subset of a Twilio Message resource.
//
// It never contains the message body, media URLs, or Twilio's error message
// text. After sendMessage selects providerRejected or uncertain, SID and
// Status are empty and the value echoes the requested account, sender, and
// recipient; ErrorCode then holds the rejection's Twilio error code.
type Message struct {
	// SID is Twilio's message identifier: SM or MM followed by 32 hexadecimal characters.
	SID string `json:"sid,omitempty"`
	// AccountSID is the AC account that owns the message.
	AccountSID string `json:"accountSid"`
	// MessagingServiceSID is the MG Messaging Service that sent the message, when one was used.
	MessagingServiceSID string `json:"messagingServiceSid,omitempty"`
	// From is the sending number, WhatsApp address, short code, or alphanumeric sender ID.
	// It is empty until a Messaging Service chooses a sender.
	From string `json:"from,omitempty"`
	// To is the recipient in E.164 form, or a whatsapp: address.
	To string `json:"to"`
	// Status is Twilio's delivery status.
	Status MessageStatus `json:"status,omitempty"`
	// Direction is Twilio's direction value, such as outbound-api.
	Direction string `json:"direction,omitempty"`
	// ErrorCode is Twilio's numeric error code, documented at https://www.twilio.com/docs/api/errors; zero means none.
	ErrorCode int `json:"errorCode,omitempty"`
	// SegmentCount is the number of SMS segments Twilio billed or will bill.
	SegmentCount int `json:"segmentCount,omitempty"`
	// MediaCount is the number of media items attached to the message.
	MediaCount int `json:"mediaCount,omitempty"`
	// Price is Twilio's decimal price, usually negative, once known.
	Price string `json:"price,omitempty"`
	// PriceUnit is the ISO 4217 currency of Price.
	PriceUnit string `json:"priceUnit,omitempty"`
	// CreatedAt is when Twilio created the message; zero means unknown.
	CreatedAt time.Time `json:"createdAt"`
	// SentAt is when Twilio sent the message; zero means not yet sent.
	SentAt time.Time `json:"sentAt"`
	// UpdatedAt is when Twilio last changed the message; zero means unknown.
	UpdatedAt time.Time `json:"updatedAt"`
}

// SendMessageInput describes one message to one recipient.
type SendMessageInput struct {
	// To is the recipient: an E.164 number such as +14155550100, or whatsapp:+14155550100.
	To string `json:"to"`
	// Body is the message text, at most 1600 characters. It may be empty only when MediaURLs is set.
	Body string `json:"body,omitempty"`
	// MediaURLs lists at most 10 public HTTPS URLs that Twilio fetches and attaches, which makes an SMS an MMS.
	MediaURLs []string `json:"mediaUrls,omitempty"`
	// Sender overrides the connection's defaultSender with an E.164 number, a whatsapp: address, or
	// an MG Messaging Service SID. Blank uses defaultSender.
	Sender string `json:"sender,omitempty"`
}

// GetMessageInput identifies one message.
type GetMessageInput struct {
	// MessageSID is the SM or MM message SID returned by sendMessage.
	MessageSID string `json:"messageSid"`
}

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient replaces the HTTP client used for Twilio API calls. The caller
// keeps ownership of client and its transport. The connector uses a copy that
// never follows redirects, and it bounds every request by 20 seconds even when
// client allows longer, so a hung send selects uncertain before Dex's
// 30-second Execute timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Twilio Messages API calls for one account.
// It is safe for concurrent use.
type Client struct {
	messagesURL      string
	accountSID       string
	defaultSender    messageSender
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

// SendMessageOperation implements the sendMessage Mutation.
type SendMessageOperation struct{ client *Client }

// GetMessageOperation implements the getMessage Query.
type GetMessageOperation struct{ client *Client }

type messageSender struct {
	fromAddress         string
	messagingServiceSID string
}

type sendRequest struct {
	recipient string
	sender    messageSender
	form      url.Values
}

type basicAuthorization struct {
	username string
	password string
}

type providerResponse struct {
	statusCode        int
	header            http.Header
	body              []byte
	isBodyTooLarge    bool
	hasBodyReadFailed bool
}

// dispatchObservation records whether a request could have reached Twilio.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

type twilioMessageResource struct {
	SID                 string          `json:"sid"`
	AccountSID          string          `json:"account_sid"`
	MessagingServiceSID *string         `json:"messaging_service_sid"`
	From                *string         `json:"from"`
	To                  string          `json:"to"`
	Status              string          `json:"status"`
	Direction           *string         `json:"direction"`
	ErrorCode           flexibleInteger `json:"error_code"`
	SegmentCount        flexibleInteger `json:"num_segments"`
	MediaCount          flexibleInteger `json:"num_media"`
	Price               *string         `json:"price"`
	PriceUnit           *string         `json:"price_unit"`
	DateCreated         *string         `json:"date_created"`
	DateSent            *string         `json:"date_sent"`
	DateUpdated         *string         `json:"date_updated"`
}

// flexibleInteger accepts a number, decimal string, or null; Twilio sends counts as strings.
type flexibleInteger int

// New validates configuration and constructs a Twilio Messages client.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	accountSID := strings.TrimSpace(config.AccountSID)
	if !accountSIDPattern.MatchString(accountSID) {
		return nil, fmt.Errorf("Twilio accountSid must be AC followed by 32 lowercase hexadecimal characters")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Twilio endpoint: %w", err)
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("Twilio maxResponseBytes must be positive")
	}
	var defaultSender messageSender
	if strings.TrimSpace(config.DefaultSender) != "" {
		defaultSender, err = parseMessageSender(config.DefaultSender)
		if err != nil {
			return nil, fmt.Errorf("Twilio defaultSender: %w", err)
		}
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Twilio connector option is nil")
		}
		option(&dependencies)
	}
	return &Client{
		messagesURL: baseURL + "/Accounts/" + accountSID + "/Messages", accountSID: accountSID, defaultSender: defaultSender,
		httpClient:  providerhttp.NewProviderHTTPClient(dependencies.httpClient, requestTimeout),
		credentials: credentials, maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}, nil
}

// SendMessage returns the sendMessage Mutation bound to this client.
func (client *Client) SendMessage() SendMessageOperation { return SendMessageOperation{client: client} }

// GetMessage returns the getMessage Query bound to this client.
func (client *Client) GetMessage() GetMessageOperation { return GetMessageOperation{client: client} }

// Definition returns the immutable connector operation definition.
func (SendMessageOperation) Definition() sdkgo.MutationDefinition { return SendMessageDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Twilio accepts no idempotency key, so it is never sent and cannot deduplicate a repeated send.
func (SendMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one message and classifies the attempt without ever retrying a dispatched request.
func (operation SendMessageOperation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[Message] {
	client := operation.client
	request, err := client.buildSendRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, Message{}, failurePointer("sendMessage", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	authorization, failure := client.resolveAuthorization(call, "sendMessage")
	if failure != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, Message{}, failure, sdkgo.Receipt{})
	}
	requested := client.requestedMessage(request)
	response, isDispatched, err := client.exchange(call, authorization, http.MethodPost, client.messagesURL+".json", request.form)
	if err != nil {
		if errors.Is(err, errRequestNotBuilt) {
			return sdkgo.NewMutationBranch(SendMessageBranchDefect, Message{}, failurePointer("sendMessage", sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		if !isDispatched {
			return sdkgo.NewMutationRetry[Message](newFailure("sendMessage", sdkgo.FailureTransport, "Twilio could not be reached, so no message was sent"), 0)
		}
		return sdkgo.NewMutationUncertain(requested, newFailure("sendMessage", sdkgo.FailureTransport, "Twilio message outcome is unknown"), client.receipt(call, providerResponse{}, "", 0))
	}
	switch {
	case response.statusCode >= 200 && response.statusCode < 300:
		if response.isBodyTooLarge {
			return sdkgo.NewMutationUncertain(requested, newFailure("sendMessage", sdkgo.FailureResponseTooLarge, "Twilio accepted the request but its response exceeds the configured size limit"), client.receipt(call, response, "", 0))
		}
		if response.hasBodyReadFailed {
			return sdkgo.NewMutationUncertain(requested, newFailure("sendMessage", sdkgo.FailureTransport, "Twilio accepted the request but its response was interrupted"), client.receipt(call, response, "", 0))
		}
		message, err := client.decodeMessage(response.body, authorization)
		if err != nil {
			return sdkgo.NewMutationUncertain(requested, newFailure("sendMessage", sdkgo.FailureProtocol, "Twilio accepted the request but returned an unusable message"), client.receipt(call, response, "", 0))
		}
		return sdkgo.NewMutationBranch(SendMessageBranchAccepted, message, nil, client.receipt(call, response, message.SID, 0))
	case response.statusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[Message](newFailure("sendMessage", sdkgo.FailureRateLimit, "Twilio rate limited the message before creating it"), providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now()))
	case response.statusCode >= 400 && response.statusCode < 500 && response.statusCode != http.StatusRequestTimeout:
		errorCode := readErrorCode(response.body)
		requested.ErrorCode = errorCode
		return sdkgo.NewMutationBranch(SendMessageBranchProviderRejected, requested, rejectionFailure("sendMessage", "message", response.statusCode, errorCode), client.receipt(call, response, "", errorCode))
	default:
		return sdkgo.NewMutationUncertain(requested, newFailure("sendMessage", sdkgo.FailureAvailability, "Twilio message outcome is unknown"), client.receipt(call, response, "", 0))
	}
}

// Definition returns the immutable connector operation definition.
func (GetMessageOperation) Definition() sdkgo.QueryDefinition { return GetMessageDefinition }

// Invoke reads one message. Transport failures, 408, 429, and 5xx responses are retried.
func (operation GetMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.QueryAttempt[Message] {
	client := operation.client
	messageSID := strings.TrimSpace(input.MessageSID)
	if !messageSIDPattern.MatchString(messageSID) {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, failurePointer("getMessage", sdkgo.FailureValidation, "message SID must be SM or MM followed by 32 lowercase hexadecimal characters"), sdkgo.Receipt{})
	}
	authorization, failure := client.resolveAuthorization(call, "getMessage")
	if failure != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, failure, sdkgo.Receipt{})
	}
	response, _, err := client.exchange(call, authorization, http.MethodGet, client.messagesURL+"/"+messageSID+".json", nil)
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, failurePointer("getMessage", sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[Message](newFailure("getMessage", sdkgo.FailureTransport, "Twilio message status is temporarily unavailable"), 0)
	}
	switch {
	case response.statusCode >= 200 && response.statusCode < 300:
		if response.hasBodyReadFailed {
			return sdkgo.NewQueryRetry[Message](newFailure("getMessage", sdkgo.FailureTransport, "Twilio message response was interrupted"), 0)
		}
		if response.isBodyTooLarge {
			return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, failurePointer("getMessage", sdkgo.FailureResponseTooLarge, "Twilio message response exceeds the configured size limit"), client.receipt(call, response, messageSID, 0))
		}
		message, err := client.decodeMessage(response.body, authorization)
		if err != nil || message.SID != messageSID {
			return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, failurePointer("getMessage", sdkgo.FailureProtocol, "Twilio returned an invalid message"), client.receipt(call, response, messageSID, 0))
		}
		return sdkgo.NewQueryBranch(GetMessageBranchFound, message, nil, client.receipt(call, response, messageSID, 0))
	case response.statusCode == http.StatusNotFound:
		return sdkgo.NewQueryBranch(GetMessageBranchNotFound, Message{}, failurePointer("getMessage", sdkgo.FailureNotFound, "Twilio has no message with that SID"), client.receipt(call, response, messageSID, readErrorCode(response.body)))
	case response.statusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[Message](newFailure("getMessage", sdkgo.FailureRateLimit, "Twilio rate limited the message query"), providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now()))
	case response.statusCode == http.StatusRequestTimeout || response.statusCode >= 500:
		return sdkgo.NewQueryRetry[Message](newFailure("getMessage", sdkgo.FailureAvailability, "Twilio message status is temporarily unavailable"), 0)
	case response.statusCode >= 400 && response.statusCode < 500:
		errorCode := readErrorCode(response.body)
		return sdkgo.NewQueryBranch(GetMessageBranchProviderRejected, Message{}, rejectionFailure("getMessage", "message query", response.statusCode, errorCode), client.receipt(call, response, messageSID, errorCode))
	default:
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, failurePointer("getMessage", sdkgo.FailureProtocol, "Twilio returned an unexpected HTTP status"), client.receipt(call, response, messageSID, 0))
	}
}

func (client *Client) buildSendRequest(input SendMessageInput) (sendRequest, error) {
	recipient := strings.TrimSpace(input.To)
	isWhatsAppRecipient := isWhatsAppAddress(recipient)
	if !phoneNumberPattern.MatchString(recipient) && !isWhatsAppRecipient {
		return sendRequest{}, fmt.Errorf("recipient must be an E.164 phone number or a whatsapp:+E.164 address")
	}
	sender := client.defaultSender
	if strings.TrimSpace(input.Sender) != "" {
		var err error
		if sender, err = parseMessageSender(input.Sender); err != nil {
			return sendRequest{}, err
		}
	}
	if sender == (messageSender{}) {
		return sendRequest{}, fmt.Errorf("sender is required: set defaultSender on the connection or sender on the input")
	}
	if sender.fromAddress != "" && isWhatsAppAddress(sender.fromAddress) != isWhatsAppRecipient {
		return sendRequest{}, fmt.Errorf("sender and recipient must both be whatsapp: addresses or both be phone numbers")
	}
	if !utf8.ValidString(input.Body) {
		return sendRequest{}, fmt.Errorf("body must be valid UTF-8")
	}
	if utf8.RuneCountInString(input.Body) > maximumBodyCharacters {
		return sendRequest{}, fmt.Errorf("body cannot exceed %d characters", maximumBodyCharacters)
	}
	if strings.TrimSpace(input.Body) == "" && len(input.MediaURLs) == 0 {
		return sendRequest{}, fmt.Errorf("body or at least one media URL is required")
	}
	if len(input.MediaURLs) > maximumMediaURLs {
		return sendRequest{}, fmt.Errorf("at most %d media URLs are allowed", maximumMediaURLs)
	}
	form := url.Values{"To": {recipient}}
	if input.Body != "" {
		form.Set("Body", input.Body)
	}
	for _, mediaURL := range input.MediaURLs {
		if err := validateMediaURL(mediaURL); err != nil {
			return sendRequest{}, err
		}
		form.Add("MediaUrl", mediaURL)
	}
	if sender.messagingServiceSID != "" {
		form.Set("MessagingServiceSid", sender.messagingServiceSID)
	} else {
		form.Set("From", sender.fromAddress)
	}
	return sendRequest{recipient: recipient, sender: sender, form: form}, nil
}

func (client *Client) resolveAuthorization(call sdkgo.Call, operationID string) (basicAuthorization, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return basicAuthorization{}, failurePointer(operationID, sdkgo.FailureAuthentication, "Twilio connection credentials are unavailable")
	}
	if err := credentials.Validate(); err != nil {
		return basicAuthorization{}, failurePointer(operationID, sdkgo.FailureAuthentication, "Twilio connection credentials are incomplete")
	}
	var authorization basicAuthorization
	switch credentials.AuthMethodID {
	case AuthTokenAuthMethodID:
		authorization = basicAuthorization{username: client.accountSID, password: credentials.AuthToken.Reveal()}
	case APIKeyAuthMethodID:
		apiKeySID := strings.TrimSpace(credentials.APIKeySID)
		if !apiKeySIDPattern.MatchString(apiKeySID) {
			return basicAuthorization{}, failurePointer(operationID, sdkgo.FailureAuthentication, "Twilio api_key_sid must be SK followed by 32 lowercase hexadecimal characters")
		}
		authorization = basicAuthorization{username: apiKeySID, password: credentials.APIKeySecret.Reveal()}
	default:
		return basicAuthorization{}, failurePointer(operationID, sdkgo.FailureAuthentication, "Twilio authentication method is not supported")
	}
	if !providerhttp.IsHeaderSafeCredential(authorization.password) {
		return basicAuthorization{}, failurePointer(operationID, sdkgo.FailureAuthentication, "Twilio credential secret is malformed")
	}
	return authorization, nil
}

// exchange performs one request; an error with isDispatched false proves Twilio received nothing.
func (client *Client) exchange(
	call sdkgo.Call,
	authorization basicAuthorization,
	method string,
	target string,
	form url.Values,
) (response providerResponse, isDispatched bool, err error) {
	requestContext, cancel := context.WithTimeout(call.Context, requestTimeout)
	defer cancel()
	observation := &dispatchObservation{}
	requestContext = httptrace.WithClientTrace(requestContext, observation.clientTrace())
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequestWithContext(requestContext, method, target, body)
	if err != nil {
		return providerResponse{}, false, errRequestNotBuilt
	}
	request.SetBasicAuth(authorization.username, authorization.password)
	request.Header.Set("Accept", "application/json")
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{}, !observation.isProvablyUndispatched(), err
	}
	defer func() {
		// The body is fully read or bounded below; a close failure cannot change the classified response.
		_ = httpResponse.Body.Close()
	}()
	response = providerResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		response.body, err = io.ReadAll(io.LimitReader(httpResponse.Body, providerhttp.MaxErrorBodyBytes))
		response.hasBodyReadFailed = err != nil
		return response, true, nil
	}
	response.body, err = providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	response.isBodyTooLarge = errors.Is(err, providerhttp.ErrBodyTooLarge)
	response.hasBodyReadFailed = err != nil && !response.isBodyTooLarge
	return response, true, nil
}

func (client *Client) decodeMessage(contents []byte, authorization basicAuthorization) (Message, error) {
	var resource twilioMessageResource
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(&resource); err != nil {
		return Message{}, errMessageResponseInvalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Message{}, errMessageResponseInvalid
	}
	if !messageSIDPattern.MatchString(resource.SID) || resource.AccountSID != client.accountSID ||
		!messageStatusPattern.MatchString(resource.Status) || !isSafeProviderAddress(resource.To) {
		return Message{}, errMessageResponseInvalid
	}
	message := Message{
		SID: resource.SID, AccountSID: resource.AccountSID, To: resource.To, Status: MessageStatus(resource.Status),
		ErrorCode: int(resource.ErrorCode), SegmentCount: int(resource.SegmentCount), MediaCount: int(resource.MediaCount),
		CreatedAt: parseTwilioTime(resource.DateCreated), SentAt: parseTwilioTime(resource.DateSent),
		UpdatedAt: parseTwilioTime(resource.DateUpdated),
	}
	if serviceSID := valueOrEmpty(resource.MessagingServiceSID); serviceSID != "" {
		if !messagingServiceSIDPattern.MatchString(serviceSID) {
			return Message{}, errMessageResponseInvalid
		}
		message.MessagingServiceSID = serviceSID
	}
	if from := valueOrEmpty(resource.From); from != "" {
		if !isSafeProviderAddress(from) {
			return Message{}, errMessageResponseInvalid
		}
		message.From = from
	}
	if direction := valueOrEmpty(resource.Direction); messageDirectionPattern.MatchString(direction) {
		message.Direction = direction
	}
	if price := valueOrEmpty(resource.Price); pricePattern.MatchString(price) {
		message.Price = price
	}
	if priceUnit := valueOrEmpty(resource.PriceUnit); priceUnitPattern.MatchString(priceUnit) {
		message.PriceUnit = priceUnit
	}
	if message.containsSecret(authorization.password) {
		return Message{}, errMessageResponseInvalid
	}
	return message, nil
}

func (client *Client) requestedMessage(request sendRequest) Message {
	return Message{
		AccountSID: client.accountSID, MessagingServiceSID: request.sender.messagingServiceSID,
		From: request.sender.fromAddress, To: request.recipient,
	}
}

func (client *Client) receipt(call sdkgo.Call, response providerResponse, messageSID string, errorCode int) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName, ProviderObjectID: messageSID,
		ObservedAt: client.now().UTC(),
	}
	if requestID := response.header.Get(requestIDHeader); isSafeProviderAddress(requestID) {
		receipt.ProviderRequestID = requestID
	}
	if errorCode > 0 {
		receipt.Metadata = map[string]string{errorCodeMetadataKey: strconv.Itoa(errorCode)}
	}
	return receipt
}

func (message Message) containsSecret(secret string) bool {
	for _, value := range []string{
		message.SID, message.AccountSID, message.MessagingServiceSID, message.From, message.To,
		string(message.Status), message.Direction, message.Price, message.PriceUnit,
	} {
		if value != "" && strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

// UnmarshalJSON accepts a non-negative JSON number, a decimal string, or null.
func (value *flexibleInteger) UnmarshalJSON(contents []byte) error {
	text := string(bytes.TrimSpace(contents))
	if text == "null" {
		*value = 0
		return nil
	}
	if unquoted, err := strconv.Unquote(text); err == nil {
		text = unquoted
	}
	if text == "" {
		*value = 0
		return nil
	}
	if !errorCodePattern.MatchString(text) {
		return fmt.Errorf("Twilio integer field is invalid")
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("Twilio integer field is invalid")
	}
	*value = flexibleInteger(parsed)
	return nil
}

func (observation *dispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone:          observation.recordDNSDone,
		ConnectDone:      observation.recordConnectDone,
		TLSHandshakeDone: observation.recordTLSHandshakeDone,
		GotConn:          observation.recordObtainedConnection,
	}
}

// isProvablyUndispatched requires a traced DNS, connect, or TLS failure and no obtained connection.
func (observation *dispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasFailedToConnect && !observation.hasObtainedConnection
}

func (observation *dispatchObservation) recordDNSDone(info httptrace.DNSDoneInfo) {
	if info.Err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordConnectDone(_ string, _ string, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordTLSHandshakeDone(_ tls.ConnectionState, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordObtainedConnection(httptrace.GotConnInfo) {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasObtainedConnection = true
}

func (observation *dispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasFailedToConnect = true
}

func parseMessageSender(value string) (messageSender, error) {
	sender := strings.TrimSpace(value)
	switch {
	case messagingServiceSIDPattern.MatchString(sender):
		return messageSender{messagingServiceSID: sender}, nil
	case phoneNumberPattern.MatchString(sender), isWhatsAppAddress(sender):
		return messageSender{fromAddress: sender}, nil
	default:
		return messageSender{}, fmt.Errorf("sender must be an E.164 phone number, a whatsapp:+E.164 address, or an MG Messaging Service SID")
	}
}

func isWhatsAppAddress(value string) bool {
	phoneNumber, hasPrefix := strings.CutPrefix(value, "whatsapp:")
	return hasPrefix && phoneNumberPattern.MatchString(phoneNumber)
}

func validateMediaURL(value string) error {
	if len(value) > maximumMediaURLBytes || !isPrintableASCIIWithoutSpaces(value) {
		return fmt.Errorf("media URLs must be printable ASCII without spaces and at most %d bytes", maximumMediaURLBytes)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("media URLs must be absolute HTTPS URLs without credentials")
	}
	return nil
}

// isSafeProviderAddress accepts a bounded printable value such as a phone number or request ID.
func isSafeProviderAddress(value string) bool {
	if value == "" || len(value) > maximumAddressBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7E {
			return false
		}
	}
	return true
}

func isPrintableASCIIWithoutSpaces(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' {
			return false
		}
	}
	return value != ""
}

func parseTwilioTime(value *string) time.Time {
	parsed, err := time.Parse(time.RFC1123Z, valueOrEmpty(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func readErrorCode(body []byte) int {
	for _, token := range providerhttp.ReadErrorTokens(body, []string{"/code"}) {
		if errorCodePattern.MatchString(token) {
			code, err := strconv.Atoi(token)
			if err == nil {
				return code
			}
		}
	}
	return 0
}

func rejectionFailure(operationID string, subject string, statusCode int, errorCode int) *sdkgo.Failure {
	kind := sdkgo.FailureProviderRejection
	switch statusCode {
	case http.StatusUnauthorized:
		kind = sdkgo.FailureAuthentication
	case http.StatusForbidden:
		kind = sdkgo.FailureAuthorization
	case http.StatusNotFound:
		kind = sdkgo.FailureNotFound
	case http.StatusConflict:
		kind = sdkgo.FailureConflict
	}
	message := "Twilio rejected the " + subject
	if errorCode > 0 {
		message += " with error " + strconv.Itoa(errorCode)
	}
	return failurePointer(operationID, kind, message)
}

func newFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func failurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := newFailure(operationID, kind, message)
	return &failure
}
