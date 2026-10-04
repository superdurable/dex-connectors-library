// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var (
	errProviderStalled       = errors.New("provider sent no bytes within the stall timeout")
	errTextStreamWriteFailed = errors.New("text stream write failed")
)

// textGenerationAttempt owns one Invoke: its call, the response being
// built, and the resolved credential.
type textGenerationAttempt struct {
	query      *TextGenerationQuery
	call       sdkgo.Call
	response   TextGenerationResponse
	credential string
}

// preparedRequest is a validated request ready for the wire format.
type preparedRequest struct {
	encodeInput EncodeRequestInput
	// originalSchema is the application's canonical schema, or nil without structured output.
	originalSchema map[string]any
}

func (attempt *textGenerationAttempt) run(request TextGenerationRequest) sdkgo.QueryAttempt[TextGenerationResponse] {
	if attempt.call.Context == nil {
		return attempt.defect(sdkgo.FailureLocalDefect, "a Dex Step context is required")
	}
	prepared, failure := attempt.prepareRequest(request)
	if failure != nil {
		return attempt.defectWithFailure(failure)
	}
	if failure := attempt.resolveCredential(); failure != nil {
		return attempt.defectWithFailure(failure)
	}
	wireFormat := &attempt.query.wireFormat
	encoded, err := wireFormat.EncodeRequest(prepared.encodeInput)
	if err != nil {
		return attempt.defect(sdkgo.FailureValidation, "the request cannot be encoded for the provider: "+err.Error())
	}
	if err := validateEncodedRequest(encoded, wireFormat.CredentialHeader.Name); err != nil {
		return attempt.defect(sdkgo.FailureLocalDefect, "the wire format built an invalid request: "+err.Error())
	}
	if (encoded.IsStreaming && wireFormat.DecodeStream == nil) || (!encoded.IsStreaming && wireFormat.DecodeResponse == nil) {
		return attempt.defect(sdkgo.FailureLocalDefect, "the wire format cannot decode the response form it requested")
	}
	return attempt.exchange(encoded, prepared.originalSchema)
}

func (attempt *textGenerationAttempt) prepareRequest(request TextGenerationRequest) (preparedRequest, *sdkgo.Failure) {
	query := attempt.query
	model := ResolveModel(request.Model, query.connectionModel)
	if model == "" {
		return preparedRequest{}, attempt.failurePointer(sdkgo.FailureValidation, "no model is selected; set the request Model or the connection model")
	}
	canonicalModel, err := query.wireFormat.ModelIDRule.ValidateModelID(model)
	if err != nil {
		return preparedRequest{}, attempt.failurePointer(sdkgo.FailureValidation, err.Error())
	}
	attempt.response.RequestedModel = canonicalModel
	if err := validateRequestFeatures(request, query.wireFormat.Features); err != nil {
		return preparedRequest{}, attempt.failurePointer(sdkgo.FailureValidation, err.Error())
	}
	if err := validateMessages(request.Messages); err != nil {
		return preparedRequest{}, attempt.failurePointer(sdkgo.FailureValidation, err.Error())
	}
	if request.MaxOutputTokens < 0 {
		return preparedRequest{}, attempt.failurePointer(sdkgo.FailureValidation, "maxOutputTokens cannot be negative")
	}
	rules := query.wireFormat.RulesForModel(canonicalModel)
	if request.Temperature != nil {
		if err := validateTemperature(*request.Temperature, rules.Temperature); err != nil {
			return preparedRequest{}, attempt.failurePointer(sdkgo.FailureValidation, err.Error())
		}
	}
	if request.ReasoningEffort != "" {
		if err := validateReasoningEffort(request.ReasoningEffort, rules.ReasoningEfforts); err != nil {
			return preparedRequest{}, attempt.failurePointer(sdkgo.FailureValidation, err.Error())
		}
	}
	prepared := preparedRequest{encodeInput: EncodeRequestInput{Request: request, Rules: rules}}
	prepared.encodeInput.Request.Model = canonicalModel
	if request.StructuredOutput != nil {
		if failure := attempt.prepareStructuredOutput(&prepared, rules.StructuredOutput); failure != nil {
			return preparedRequest{}, failure
		}
	}
	return prepared, nil
}

func (attempt *textGenerationAttempt) prepareStructuredOutput(prepared *preparedRequest, rules StructuredOutputRules) *sdkgo.Failure {
	output := *prepared.encodeInput.Request.StructuredOutput
	if err := rules.Validate(); err != nil {
		return attempt.failurePointer(sdkgo.FailureLocalDefect, "the wire format's structured output rules are invalid: "+err.Error())
	}
	if rules.Mode == StructuredOutputModeNone {
		return attempt.failurePointer(sdkgo.FailureValidation, "the model does not support structured output")
	}
	if rules.Mode == StructuredOutputModeJSONObjectWithInstruction && !attempt.query.wireFormat.Features.SupportsInstructions {
		return attempt.failurePointer(sdkgo.FailureLocalDefect, "the JSON object mode requires the Instructions feature")
	}
	if !structuredOutputNamePattern.MatchString(output.Name) {
		return attempt.failurePointer(sdkgo.FailureValidation, "structured output name must match "+structuredOutputNamePattern.String())
	}
	if output.Schema == nil {
		return attempt.failurePointer(sdkgo.FailureValidation, "structured output schema is required")
	}
	canonical, err := canonicalizeSchema(output.Schema)
	if err != nil {
		return attempt.failurePointer(sdkgo.FailureValidation, "structured output "+err.Error())
	}
	if err := checkPortableSchema(canonical); err != nil {
		return attempt.failurePointer(sdkgo.FailureValidation, "structured output schema is not portable "+err.Error())
	}
	providerSchema, err := transformSchemaForProvider(canonical, rules)
	if err != nil {
		return attempt.failurePointer(sdkgo.FailureValidation, "structured output "+err.Error())
	}
	prepared.originalSchema = canonical
	prepared.encodeInput.Request.StructuredOutput = &StructuredOutput{
		Name: output.Name, Description: output.Description, Schema: providerSchema,
	}
	if rules.Mode == StructuredOutputModeJSONObjectWithInstruction {
		instruction, err := jsonObjectInstruction(output, canonical)
		if err != nil {
			return attempt.failurePointer(sdkgo.FailureValidation, "structured output "+err.Error())
		}
		instructions := prepared.encodeInput.Request.Instructions
		if instructions != "" {
			instructions += "\n\n"
		}
		prepared.encodeInput.Request.Instructions = instructions + instruction
	}
	return nil
}

func (attempt *textGenerationAttempt) resolveCredential() *sdkgo.Failure {
	secret, err := attempt.query.resolveCredential(attempt.call)
	if err != nil {
		return attempt.failurePointer(sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	credential := secret.Reveal()
	if !providerhttp.IsHeaderSafeCredential(credential) {
		return attempt.failurePointer(sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	attempt.credential = credential
	return nil
}

func (attempt *textGenerationAttempt) exchange(encoded EncodedRequest, originalSchema map[string]any) sdkgo.QueryAttempt[TextGenerationResponse] {
	query := attempt.query
	attemptContext, cancelAttempt := context.WithCancelCause(attempt.call.Context)
	defer cancelAttempt(nil)
	request, err := http.NewRequestWithContext(attemptContext, http.MethodPost, query.baseURL+encoded.Path, bytes.NewReader(encoded.Body))
	if err != nil {
		return attempt.defect(sdkgo.FailureLocalDefect, "the provider request could not be built")
	}
	for name, values := range encoded.Header {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if encoded.IsStreaming {
		request.Header.Set("Accept", "text/event-stream")
	}
	credentialHeader := query.wireFormat.CredentialHeader
	request.Header.Set(credentialHeader.Name, credentialHeader.Prefix+attempt.credential)

	heartbeat := startAttemptHeartbeat(attempt.call, query.heartbeatInterval)
	defer heartbeat.stop()
	stall := startStallWatchdog(query.wireFormat.StallTimeout, cancelAttempt)
	defer stall.stop()

	response, err := query.httpClient.Do(request)
	if err != nil {
		if errors.Is(context.Cause(attemptContext), errProviderStalled) {
			return attempt.retry(sdkgo.FailureAvailability, errProviderStalled.Error(), 0)
		}
		return attempt.retry(sdkgo.FailureTransport, "provider request failed before a response was received", 0)
	}
	defer response.Body.Close()
	stall.observeProgress()
	body := &providerResponseBody{body: response.Body, stall: stall}
	receipt := attempt.receipt(response.Header)
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return attempt.classifyErrorStatus(response, body, receipt)
	}
	// A gateway that ignores streaming sends a complete body, which is not an interrupted stream.
	isEventStream := encoded.IsStreaming && isEventStreamMediaType(response.Header.Get("Content-Type"))
	if !isEventStream && query.wireFormat.DecodeResponse == nil {
		return attempt.branch(InvalidResponseBranchID, sdkgo.FailureProtocol, "provider did not return the event stream it was asked for", receipt)
	}
	var decoded DecodedResponse
	if isEventStream {
		events := providerhttp.NewServerSentEventReader(body, query.maxResponseBytes, query.maxStreamEventBytes)
		decoded, err = query.wireFormat.DecodeStream(events, attempt.writeTextDelta)
	} else {
		var contents []byte
		if contents, err = providerhttp.ReadBoundedBody(body, query.maxResponseBytes); err == nil {
			decoded, err = query.wireFormat.DecodeResponse(contents)
		}
	}
	if err != nil {
		return attempt.classifyDecodeFailure(err, isEventStream, attemptContext, body, receipt)
	}
	return attempt.completeResponse(decoded, originalSchema, isEventStream, receipt)
}

func (attempt *textGenerationAttempt) classifyErrorStatus(response *http.Response, body io.Reader, receipt sdkgo.Receipt) sdkgo.QueryAttempt[TextGenerationResponse] {
	wireFormat := &attempt.query.wireFormat
	errorBody, err := io.ReadAll(io.LimitReader(body, providerhttp.MaxErrorBodyBytes))
	if err != nil {
		// An unreadable error body leaves the status alone to classify the response.
		errorBody = nil
	}
	tokens := attempt.safeErrorTokens(providerhttp.ReadErrorTokens(errorBody, wireFormat.ErrorTokenPointers))
	outcome := classifyErrorResponse(wireFormat.ErrorRules, response.StatusCode, tokens)
	message := appendErrorTokens("provider returned HTTP "+strconv.Itoa(response.StatusCode), tokens)
	var delay time.Duration
	if outcome.disposition == errorDispositionRetry {
		if wireFormat.ReadErrorRetryDelay != nil {
			delay = min(max(wireFormat.ReadErrorRetryDelay(errorBody), 0), providerhttp.MaxRetryAfterDelay)
		}
		if delay == 0 {
			delay = providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		}
	}
	return attempt.errorOutcome(outcome, message, delay, receipt)
}

func (attempt *textGenerationAttempt) classifyDecodeFailure(
	err error, isEventStream bool, attemptContext context.Context, body *providerResponseBody, receipt sdkgo.Receipt,
) sdkgo.QueryAttempt[TextGenerationResponse] {
	var reported *ProviderReportedError
	switch {
	case errors.As(err, &reported):
		return attempt.classifyReportedError(reported, receipt)
	case errors.Is(context.Cause(attemptContext), errProviderStalled):
		return attempt.retry(sdkgo.FailureAvailability, errProviderStalled.Error(), 0)
	case errors.Is(err, errTextStreamWriteFailed):
		return attempt.retry(sdkgo.FailureAvailability, "the text stream could not be written", 0)
	case errors.Is(err, providerhttp.ErrBodyTooLarge), errors.Is(err, providerhttp.ErrStreamTooLarge),
		errors.Is(err, providerhttp.ErrStreamEventTooLarge):
		return attempt.branch(InvalidResponseBranchID, sdkgo.FailureResponseTooLarge, "provider response exceeds the configured size limit", receipt)
	case body.readError != nil, isEventStream && errors.Is(err, io.ErrUnexpectedEOF):
		return attempt.retry(sdkgo.FailureTransport, "provider response ended before it was complete", 0)
	default:
		return attempt.branch(InvalidResponseBranchID, sdkgo.FailureProtocol, "provider response does not match its documented format", receipt)
	}
}

// classifyReportedError classifies an error object the provider sent inside a 2xx response.
func (attempt *textGenerationAttempt) classifyReportedError(reported *ProviderReportedError, receipt sdkgo.Receipt) sdkgo.QueryAttempt[TextGenerationResponse] {
	tokens := attempt.safeErrorTokens(reported.ErrorTokens)
	outcome := classifyReportedError(attempt.query.wireFormat.ErrorRules, tokens)
	return attempt.errorOutcome(outcome, appendErrorTokens(reported.Error(), tokens), 0, receipt)
}

func (attempt *textGenerationAttempt) completeResponse(
	decoded DecodedResponse, originalSchema map[string]any, isStreaming bool, receipt sdkgo.Receipt,
) sdkgo.QueryAttempt[TextGenerationResponse] {
	if !isBoundedIdentifier(decoded.ServedModel) || !isBoundedIdentifier(decoded.ResponseID) ||
		attempt.revealsCredential(decoded.ServedModel) || attempt.revealsCredential(decoded.ResponseID) {
		return attempt.branch(InvalidResponseBranchID, sdkgo.FailureProtocol, "provider returned an invalid model or response identifier", receipt)
	}
	attempt.response.ServedModel, attempt.response.ResponseID = decoded.ServedModel, decoded.ResponseID
	receipt.ProviderObjectID = decoded.ResponseID
	usage, err := completeUsage(decoded.Usage)
	if err != nil {
		return attempt.branch(InvalidResponseBranchID, sdkgo.FailureProtocol, err.Error(), receipt)
	}
	attempt.response.Usage = usage
	if attempt.revealsCredential(decoded.ProviderFinishReason) {
		return attempt.branch(InvalidResponseBranchID, sdkgo.FailureProtocol, "provider returned an invalid finish reason", receipt)
	}
	if providerTokenPattern.MatchString(decoded.ProviderFinishReason) {
		attempt.response.ProviderFinishReason = decoded.ProviderFinishReason
	}
	switch {
	case decoded.IsRefusal:
		attempt.response.FinishReason = FinishReasonRefusal
	case attempt.response.ProviderFinishReason == "":
		return attempt.branch(InvalidResponseBranchID, sdkgo.FailureProtocol, "provider returned no recognizable finish reason", receipt)
	default:
		reason, isKnown := attempt.query.wireFormat.FinishReasons[attempt.response.ProviderFinishReason]
		if !isKnown {
			return attempt.branch(InvalidResponseBranchID, sdkgo.FailureProtocol,
				"provider stopped for an unsupported reason: "+attempt.response.ProviderFinishReason, receipt)
		}
		attempt.response.FinishReason = reason
	}
	var text strings.Builder
	for _, part := range decoded.Parts {
		if !part.IsReasoning {
			text.WriteString(part.Text)
		}
	}
	switch attempt.response.FinishReason {
	case FinishReasonStop:
		generatedText := text.String()
		if generatedText == "" {
			return attempt.branch(InvalidResponseBranchID, sdkgo.FailureProtocol, "provider finished without text", receipt)
		}
		if originalSchema != nil {
			generatedText = stripJSONFence(generatedText)
			if err := validateStructuredOutputText(generatedText, originalSchema); err != nil {
				return attempt.branch(InvalidResponseBranchID, sdkgo.FailureProtocol, "structured output does not match its schema "+err.Error(), receipt)
			}
		}
		return attempt.textBranch(GeneratedBranchID, generatedText, isStreaming, nil, receipt)
	case FinishReasonLength:
		failure := attempt.failurePointer(sdkgo.FailureResponseTooLarge, "provider stopped at the output token limit")
		return attempt.textBranch(TruncatedBranchID, text.String(), isStreaming, failure, receipt)
	case FinishReasonRefusal:
		return attempt.branch(BlockedBranchID, sdkgo.FailureProviderRejection, "the model refused the request", receipt)
	default:
		return attempt.branch(BlockedBranchID, sdkgo.FailureProviderRejection, "provider stopped the response for a content policy", receipt)
	}
}

// textBranch returns a text-bearing branch, writing the text to the Stream unless it was streamed.
func (attempt *textGenerationAttempt) textBranch(
	branch sdkgo.BranchID, text string, isStreaming bool, failure *sdkgo.Failure, receipt sdkgo.Receipt,
) sdkgo.QueryAttempt[TextGenerationResponse] {
	if !isStreaming && text != "" {
		if err := attempt.writeTextDelta(text); err != nil {
			return attempt.retry(sdkgo.FailureAvailability, "the text stream could not be written", 0)
		}
	}
	attempt.response.Text = text
	return sdkgo.NewQueryBranch(branch, attempt.response, failure, receipt)
}

func (attempt *textGenerationAttempt) writeTextDelta(delta string) error {
	if delta == "" || !attempt.call.HasTextStream() {
		return nil
	}
	if err := attempt.call.WriteText(delta); err != nil {
		return fmt.Errorf("%w: %w", errTextStreamWriteFailed, err)
	}
	return nil
}

func (attempt *textGenerationAttempt) receipt(header http.Header) sdkgo.Receipt {
	wireFormat := &attempt.query.wireFormat
	receipt := sdkgo.Receipt{CallID: attempt.call.ID, Provider: wireFormat.ProviderName, ObservedAt: time.Now().UTC()}
	for _, name := range wireFormat.RequestIDHeaders {
		if value := strings.TrimSpace(header.Get(name)); value != "" && isBoundedIdentifier(value) && !attempt.revealsCredential(value) {
			receipt.ProviderRequestID = value
			break
		}
	}
	for _, name := range wireFormat.RateLimitHeaders {
		value := strings.TrimSpace(header.Get(name))
		if value == "" || len(value) > 256 || !isPrintableASCII(value) || attempt.revealsCredential(value) {
			continue
		}
		if receipt.Metadata == nil {
			receipt.Metadata = map[string]string{}
		}
		receipt.Metadata[strings.ToLower(name)] = value
	}
	return receipt
}

func (attempt *textGenerationAttempt) errorOutcome(
	outcome ErrorOutcome, message string, delay time.Duration, receipt sdkgo.Receipt,
) sdkgo.QueryAttempt[TextGenerationResponse] {
	switch outcome.disposition {
	case errorDispositionRetry:
		return attempt.retry(outcome.kind, message, delay)
	case errorDispositionInvalidResponse:
		return attempt.branch(InvalidResponseBranchID, outcome.kind, message, receipt)
	case errorDispositionBlocked:
		attempt.response.FinishReason = FinishReasonContentPolicy
		return attempt.branch(BlockedBranchID, outcome.kind, message, receipt)
	default:
		return attempt.branch(ProviderRejectedBranchID, outcome.kind, message, receipt)
	}
}

// safeErrorTokens keeps only pattern-bounded tokens that do not contain the credential.
func (attempt *textGenerationAttempt) safeErrorTokens(tokens []string) []string {
	var safeTokens []string
	for _, token := range tokens {
		if providerTokenPattern.MatchString(token) && !attempt.revealsCredential(token) {
			safeTokens = append(safeTokens, token)
		}
	}
	return safeTokens
}

func (attempt *textGenerationAttempt) revealsCredential(value string) bool {
	return attempt.credential != "" && strings.Contains(value, attempt.credential)
}

func (attempt *textGenerationAttempt) branch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.QueryAttempt[TextGenerationResponse] {
	return sdkgo.NewQueryBranch(branch, attempt.response, attempt.failurePointer(kind, message), receipt)
}

func (attempt *textGenerationAttempt) retry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[TextGenerationResponse] {
	return sdkgo.NewQueryRetry[TextGenerationResponse](attempt.failure(kind, message), delay)
}

func (attempt *textGenerationAttempt) defect(kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TextGenerationResponse] {
	return attempt.defectWithFailure(attempt.failurePointer(kind, message))
}

func (attempt *textGenerationAttempt) defectWithFailure(failure *sdkgo.Failure) sdkgo.QueryAttempt[TextGenerationResponse] {
	receipt := sdkgo.Receipt{Provider: attempt.query.wireFormat.ProviderName}
	return sdkgo.NewQueryBranch(DefectBranchID, attempt.response, failure, receipt)
}

func (attempt *textGenerationAttempt) failurePointer(kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := attempt.failure(kind, message)
	return &failure
}

func (attempt *textGenerationAttempt) failure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{
		Kind: kind, Provider: attempt.query.wireFormat.ProviderName,
		Operation: attempt.query.definition.Operation.OperationID, Message: message,
	}
}

func appendErrorTokens(message string, tokens []string) string {
	if len(tokens) == 0 {
		return message
	}
	return message + " " + strings.Join(tokens, " ")
}

func validateRequestFeatures(request TextGenerationRequest, features RequestFeatures) error {
	unsupported := ""
	switch {
	// The message names the request field, not the RequestFeatures flag.
	case request.Instructions != "" && !features.SupportsInstructions:
		unsupported = "Instructions"
	case request.StructuredOutput != nil && !features.SupportsStructuredOutput:
		unsupported = "StructuredOutput"
	case request.MaxOutputTokens != 0 && !features.SupportsMaxOutputTokens:
		unsupported = "MaxOutputTokens"
	case request.Temperature != nil && !features.SupportsTemperature:
		unsupported = "Temperature"
	case request.ReasoningEffort != "" && !features.SupportsReasoningEffort:
		unsupported = "ReasoningEffort"
	default:
		return nil
	}
	return fmt.Errorf("this connector's wire format does not send %s; leave it unset", unsupported)
}

func validateMessages(messages []Message) error {
	if len(messages) == 0 {
		return fmt.Errorf("at least one message is required")
	}
	for _, message := range messages {
		if message.Role != MessageRoleUser && message.Role != MessageRoleAssistant {
			return fmt.Errorf("message role must be user or assistant")
		}
		if message.Text == "" {
			return fmt.Errorf("every message requires text")
		}
	}
	return nil
}

func validateReasoningEffort(effort ReasoningEffort, accepted map[ReasoningEffort]string) error {
	switch effort {
	case ReasoningEffortNone, ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium,
		ReasoningEffortHigh, ReasoningEffortExtraHigh, ReasoningEffortMax:
	default:
		return fmt.Errorf("reasoning effort must be none, minimal, low, medium, high, xhigh, or max")
	}
	if _, isAccepted := accepted[effort]; !isAccepted {
		return fmt.Errorf("the model does not accept reasoning effort %q", effort)
	}
	return nil
}

func completeUsage(usage Usage) (Usage, error) {
	if usage.InputTokens < 0 || usage.CachedInputTokens < 0 || usage.OutputTokens < 0 ||
		usage.ReasoningTokens < 0 || usage.TotalTokens < 0 {
		return Usage{}, fmt.Errorf("provider reported negative token usage")
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage, nil
}

// isBoundedIdentifier accepts empty or 1-256 bytes of printable ASCII without spaces.
func isBoundedIdentifier(value string) bool {
	return value == "" || (len(value) <= 256 && providerhttp.IsHeaderSafeCredential(value))
}

func isPrintableASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < ' ' || value[index] > '~' {
			return false
		}
	}
	return true
}

func isEventStreamMediaType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "text/event-stream"
}

// attemptHeartbeat records a nil Dex heartbeat on a timer while one attempt is in flight.
type attemptHeartbeat struct {
	stopSignal chan struct{}
	stopped    chan struct{}
}

func startAttemptHeartbeat(call sdkgo.Call, interval time.Duration) *attemptHeartbeat {
	heartbeat := &attemptHeartbeat{stopSignal: make(chan struct{}), stopped: make(chan struct{})}
	go heartbeat.recordUntilStopped(call, interval)
	return heartbeat
}

func (heartbeat *attemptHeartbeat) recordUntilStopped(call sdkgo.Call, interval time.Duration) {
	defer close(heartbeat.stopped)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-heartbeat.stopSignal:
			return
		case <-call.Context.Done():
			return
		case <-ticker.C:
			// A failed heartbeat only risks a heartbeat timeout; the attempt continues.
			_ = call.Context.RecordHeartbeat(nil)
		}
	}
}

// stop returns after the last heartbeat, because Dex rejects heartbeats after Execute returns.
func (heartbeat *attemptHeartbeat) stop() {
	close(heartbeat.stopSignal)
	<-heartbeat.stopped
}

// stallWatchdog cancels an attempt when no response byte arrives within its timeout.
type stallWatchdog struct {
	timeout time.Duration
	timer   *time.Timer
}

func startStallWatchdog(timeout time.Duration, cancelAttempt context.CancelCauseFunc) *stallWatchdog {
	if timeout <= 0 {
		return &stallWatchdog{}
	}
	return &stallWatchdog{
		timeout: timeout, timer: time.AfterFunc(timeout, func() { cancelAttempt(errProviderStalled) }),
	}
}

func (watchdog *stallWatchdog) observeProgress() {
	if watchdog.timer != nil {
		watchdog.timer.Reset(watchdog.timeout)
	}
}

func (watchdog *stallWatchdog) stop() {
	if watchdog.timer != nil {
		watchdog.timer.Stop()
	}
}

// providerResponseBody feeds the stall watchdog and remembers transport read failures.
type providerResponseBody struct {
	body      io.Reader
	stall     *stallWatchdog
	readError error
}

// Read reads from the provider body, resetting the stall timer on every byte.
func (body *providerResponseBody) Read(buffer []byte) (int, error) {
	count, err := body.body.Read(buffer)
	if count > 0 {
		body.stall.observeProgress()
	}
	if err != nil && !errors.Is(err, io.EOF) {
		body.readError = err
	}
	return count, err
}
