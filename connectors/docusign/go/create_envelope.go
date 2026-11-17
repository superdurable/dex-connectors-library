// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createEnvelopeFromTemplateOperationID = "createEnvelopeFromTemplate"
	// createRequestTimeout leaves time within the 60-second Execute timeout to read the envelope back.
	createRequestTimeout = 40 * time.Second
	// recoveryLookbackMargin widens the read-back window for clock skew between the Worker and DocuSign.
	recoveryLookbackMargin = time.Hour
	// minimumRecoveryRequestTime is the least time worth spending on an in-attempt read-back.
	minimumRecoveryRequestTime = 2 * time.Second

	maximumTemplateRoles        = 100
	maximumRoleTextLength       = 100
	maximumEmailSubjectLength   = 100
	maximumEmailBlurbLength     = 10000
	maximumRoutingOrder         = 999
	maximumCustomFields         = 10
	maximumCustomFieldName      = 50
	maximumCustomFieldValue     = 100
	docusignStatusDateTimeField = "statusDateTime"
)

// CreateEnvelopeFromTemplateInput describes one envelope created from a DocuSign server template.
type CreateEnvelopeFromTemplateInput struct {
	// TemplateID is the server template's GUID, shown in DocuSign under Templates > the template >
	// Template ID.
	TemplateID string `json:"templateId"`
	// TemplateRoles fills the template's roles by name, 1 to 100 of them. Every role the template
	// requires must be filled.
	TemplateRoles []TemplateRole `json:"templateRoles"`
	// EmailSubject overrides the template's email subject, at most 100 characters; blank keeps it.
	EmailSubject string `json:"emailSubject,omitempty"`
	// EmailBlurb overrides the template's email message, at most 10,000 characters; blank keeps it.
	EmailBlurb string `json:"emailBlurb,omitempty"`
	// IsDraft creates the envelope with status created and sends nothing; false sends it at once.
	IsDraft bool `json:"isDraft,omitempty"`
	// CustomFields adds hidden text custom fields, at most 10, such as a correlation ID that Connect
	// events return. Names are 1 to 50 characters and unique; dexIdempotencyKey is reserved.
	CustomFields []EnvelopeCustomField `json:"customFields,omitempty"`
}

// TemplateRole assigns one person to a template role.
type TemplateRole struct {
	// RoleName is the template's role name, such as Customer, matched as DocuSign matches it.
	RoleName string `json:"roleName"`
	// Name is the recipient's full name, 1 to 100 characters.
	Name string `json:"name"`
	// Email is the recipient's email address, at most 100 characters.
	Email string `json:"email"`
	// RoutingOrder overrides the role's routing order, 1 to 999; zero keeps the template's order, which
	// decides who is asked to sign first.
	RoutingOrder int `json:"routingOrder,omitempty"`
}

// CreatedEnvelope identifies the envelope that createEnvelopeFromTemplate created.
type CreatedEnvelope struct {
	// EnvelopeID is the new envelope's GUID.
	EnvelopeID string `json:"envelopeId"`
	// Status is sent, or created for a draft.
	Status EnvelopeStatus `json:"status"`
	// StatusChangedAt is when DocuSign recorded Status, when it said.
	StatusChangedAt *time.Time `json:"statusChangedAt,omitempty"`
	// WasRecovered is true when an attempt whose answer was lost created the envelope and this attempt
	// found it by its dexIdempotencyKey custom field instead of creating another.
	WasRecovered bool `json:"wasRecovered,omitempty"`
}

// CreateEnvelopeFromTemplateOperation implements the createEnvelopeFromTemplate Mutation.
type CreateEnvelopeFromTemplateOperation struct{ client *Client }

// createDispatchCheckpoint is the heartbeat value recorded before the create request leaves the Worker.
type createDispatchCheckpoint struct {
	IsDispatched bool      `json:"isDispatched"`
	DispatchedAt time.Time `json:"dispatchedAt"`
}

type docusignTemplateRole struct {
	RoleName     string `json:"roleName"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	RoutingOrder string `json:"routingOrder,omitempty"`
}

type docusignTextCustomField struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Show     string `json:"show"`
	Required string `json:"required"`
}

type docusignCreateEnvelopeRequest struct {
	TemplateID    string                 `json:"templateId"`
	TemplateRoles []docusignTemplateRole `json:"templateRoles"`
	Status        EnvelopeStatus         `json:"status"`
	EmailSubject  string                 `json:"emailSubject,omitempty"`
	EmailBlurb    string                 `json:"emailBlurb,omitempty"`
	CustomFields  struct {
		TextCustomFields []docusignTextCustomField `json:"textCustomFields"`
	} `json:"customFields"`
}

// Definition returns the immutable connector operation definition.
func (CreateEnvelopeFromTemplateOperation) Definition() sdkgo.MutationDefinition {
	return CreateEnvelopeFromTemplateDefinition
}

// IdempotencyKey derives the key from the stable call ID. DocuSign accepts no idempotency key, so the
// connector stores it in the envelope's hidden dexIdempotencyKey custom field and finds the envelope by
// it after an attempt whose answer was lost.
func (CreateEnvelopeFromTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEnvelopeFromTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the envelope and guards against a second one. It records a dispatch checkpoint
// before the POST. Every attempt after the first reads the envelope back by its custom field before
// sending, because Dex does not confirm that a heartbeat was stored. Each attempt sends at most once,
// so attempt - 1 bounds the earlier sends: an unknown outcome resends on a later attempt while that
// bound stays below call.MaxSends(), and selects uncertain at the cap.
func (operation CreateEnvelopeFromTemplateOperation) Invoke(
	call sdkgo.Call, input CreateEnvelopeFromTemplateInput,
) sdkgo.MutationAttempt[CreatedEnvelope] {
	client := operation.client
	marker := string(call.IdempotencyKey)
	if marker == "" {
		marker = string(call.ID)
	}
	request, err := buildCreateEnvelopeRequest(input, marker)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateEnvelopeFromTemplateBranchDefect, CreatedEnvelope{}, docusignFailurePointer(createEnvelopeFromTemplateOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	checkpoint, hasCheckpoint := earlierCreateDispatch(call)
	sends := newCreateSendBudget(call)
	session, sessionFailure := client.openSession(call, createEnvelopeFromTemplateOperationID)
	switch {
	case sessionFailure != nil && (hasCheckpoint || sends.hasPossibleEarlierSend()):
		return client.reportSessionFailureAfterPossibleSend(call, checkpoint, hasCheckpoint, sends, sessionFailure)
	case sessionFailure != nil:
		return mutationSessionFailure[CreatedEnvelope](sessionFailure, CreateEnvelopeFromTemplateBranchProviderRejected, CreateEnvelopeFromTemplateBranchProviderRejected, CreateEnvelopeFromTemplateBranchDefect)
	case hasCheckpoint || sends.hasPossibleEarlierSend():
		if attempt, isDecided := client.findEnvelopeBeforeResend(call, &session, marker, checkpoint, hasCheckpoint, sends); isDecided {
			return attempt
		}
	}
	if !hasCheckpoint {
		checkpoint = createDispatchCheckpoint{IsDispatched: true, DispatchedAt: client.now().UTC()}
	}
	if err := call.Context.RecordHeartbeat(checkpoint); err != nil {
		return sdkgo.NewMutationRetry[CreatedEnvelope](docusignFailure(createEnvelopeFromTemplateOperationID, sdkgo.FailureAvailability, "the create checkpoint could not be recorded, so nothing was sent to DocuSign"), 0)
	}
	var isDispatched atomic.Bool
	response, err := client.sendAPIRequest(call, &session, docusignRequest{
		method: http.MethodPost, path: "/envelopes", body: request, timeout: createRequestTimeout,
	}, &isDispatched)
	switch {
	case errors.Is(err, errDocuSignRequestInvalid):
		clearCreateDispatch(call)
		return sdkgo.NewMutationBranch(CreateEnvelopeFromTemplateBranchDefect, CreatedEnvelope{}, docusignFailurePointer(createEnvelopeFromTemplateOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	case err != nil && !errors.Is(err, errDocuSignResponseTooLarge) && !isDispatched.Load():
		releaseCreateDispatch(call, checkpoint, hasCheckpoint)
		return sdkgo.NewMutationRetry[CreatedEnvelope](docusignFailure(createEnvelopeFromTemplateOperationID, sdkgo.FailureTransport, "DocuSign could not be reached, so no envelope was created"), 0)
	case err != nil:
		return client.recoverCreatedEnvelope(call, &session, marker, checkpoint, sends,
			docusignFailure(createEnvelopeFromTemplateOperationID, sdkgo.FailureTransport, "DocuSign's answer to the create request was lost"))
	case response.statusCode == http.StatusCreated || response.statusCode == http.StatusOK:
		created, decodeErr := decodeCreatedEnvelope(response.body, input.IsDraft)
		if decodeErr != nil {
			return client.recoverCreatedEnvelope(call, &session, marker, checkpoint, sends,
				docusignFailure(createEnvelopeFromTemplateOperationID, sdkgo.FailureProtocol, "DocuSign accepted the create request but returned an unusable envelope summary"))
		}
		return sdkgo.NewMutationBranch(CreateEnvelopeFromTemplateBranchCreated, created, nil, client.receipt(created.EnvelopeID, ""))
	}
	outcome := client.classifyDocuSignFailure(createEnvelopeFromTemplateOperationID, response)
	switch {
	case outcome.isRetry && response.statusCode < 500 && response.statusCode != http.StatusRequestTimeout:
		// A rate-limit refusal is answered before DocuSign processes the request.
		releaseCreateDispatch(call, checkpoint, hasCheckpoint)
		return sdkgo.NewMutationRetry[CreatedEnvelope](outcome.failure, outcome.retryAfter)
	case isDefiniteDocuSignRefusal(response.statusCode) && hasCheckpoint:
		// This send was refused, but an earlier attempt's create may still exist.
		return sdkgo.NewMutationUncertain(CreatedEnvelope{}, outcome.failure, client.receipt("", outcome.errorCode))
	case isDefiniteDocuSignRefusal(response.statusCode):
		return sdkgo.NewMutationBranch(CreateEnvelopeFromTemplateBranchProviderRejected, CreatedEnvelope{}, &outcome.failure, client.receipt("", outcome.errorCode))
	default:
		// A 3xx, 408, 409, other 4xx, or 5xx can follow a create DocuSign already applied.
		return client.recoverCreatedEnvelope(call, &session, marker, checkpoint, sends, outcome.failure)
	}
}

// createSendBudget bounds possibly effective creates: each attempt sends at most once, so attempt - 1
// bounds the earlier ones whatever checkpoints Dex kept.
type createSendBudget struct {
	attempt  int32
	maxSends int32
}

func newCreateSendBudget(call sdkgo.Call) createSendBudget {
	return createSendBudget{attempt: call.Context.Attempt(), maxSends: call.MaxSends()}
}

// hasPossibleEarlierSend reports whether an earlier attempt of this Step execution may have sent the create.
func (budget createSendBudget) hasPossibleEarlierSend() bool {
	return budget.attempt > 1
}

// canSendNow reports whether this attempt may send without exceeding the cap.
func (budget createSendBudget) canSendNow() bool {
	return budget.attempt-1 < budget.maxSends
}

// canResendLater reports whether a later attempt may send after this one possibly sent.
func (budget createSendBudget) canResendLater() bool {
	return budget.attempt < budget.maxSends
}

// isDefiniteDocuSignRefusal reports a status that conclusively refuses the request unprocessed.
func isDefiniteDocuSignRefusal(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// reportSessionFailureAfterPossibleSend never claims nothing was created, since an earlier attempt may have sent.
func (client *Client) reportSessionFailureAfterPossibleSend(
	call sdkgo.Call, checkpoint createDispatchCheckpoint, hasCheckpoint bool, sends createSendBudget, failure *sessionFailure,
) sdkgo.MutationAttempt[CreatedEnvelope] {
	if hasCheckpoint {
		// The checkpoint must survive this attempt so a later one still reads back.
		_ = call.Context.RecordHeartbeat(checkpoint) // A lost re-record leaves the prior value in place.
	}
	if failure.kind == sessionFailureRetry {
		return sdkgo.NewMutationRetry[CreatedEnvelope](failure.failure, failure.retryAfter)
	}
	unknownOutcome := failure.failure
	unknownOutcome.Message = "an earlier attempt may have sent the DocuSign create request and its outcome is unknown; " + unknownOutcome.Message
	return client.resendLaterOrSelectUncertain(sends.canResendLater(), unknownOutcome)
}

// resendLaterOrSelectUncertain returns Retry while a later attempt may resend, and uncertain at the cap.
func (client *Client) resendLaterOrSelectUncertain(canResendLater bool, unknownOutcome sdkgo.Failure) sdkgo.MutationAttempt[CreatedEnvelope] {
	if canResendLater {
		// The checkpoint stays the last heartbeat, so the next attempt reads back before it sends.
		return sdkgo.NewMutationRetry[CreatedEnvelope](unknownOutcome, 0)
	}
	return sdkgo.NewMutationUncertain(CreatedEnvelope{}, unknownOutcome, client.receipt("", ""))
}

// recoverCreatedEnvelope reads back after this attempt's unknown send and adopts exactly one tagged envelope.
func (client *Client) recoverCreatedEnvelope(
	call sdkgo.Call, session *docusignSession, marker string, checkpoint createDispatchCheckpoint,
	sends createSendBudget, unknownOutcome sdkgo.Failure,
) sdkgo.MutationAttempt[CreatedEnvelope] {
	timeout, canRead := recoveryRequestTimeout(call)
	if !canRead {
		return client.resendLaterOrSelectUncertain(sends.canResendLater(), unknownOutcome)
	}
	response, err := client.listTaggedEnvelopes(call, session, marker, checkpoint.DispatchedAt, timeout)
	if err != nil || response.statusCode != http.StatusOK {
		return client.resendLaterOrSelectUncertain(sends.canResendLater(), unknownOutcome)
	}
	matches, err := findTaggedEnvelopes(response.body, marker)
	switch {
	case err == nil && len(matches) == 1:
		return client.adoptRecoveredEnvelope(matches[0])
	case err == nil && len(matches) > 1:
		return client.selectDuplicateEnvelopesUncertain()
	}
	// Listings can lag, so a miss is not proof; the next attempt reads back again before it sends.
	return client.resendLaterOrSelectUncertain(sends.canResendLater(), unknownOutcome)
}

// findEnvelopeBeforeResend reads back before a later attempt sends; a miss or an inconclusive
// read-back lets it send below the cap and selects uncertain at the cap.
func (client *Client) findEnvelopeBeforeResend(
	call sdkgo.Call, session *docusignSession, marker string, checkpoint createDispatchCheckpoint,
	hasCheckpoint bool, sends createSendBudget,
) (sdkgo.MutationAttempt[CreatedEnvelope], bool) {
	since := call.Context.FirstAttemptAt()
	if hasCheckpoint {
		since = checkpoint.DispatchedAt
	}
	unknownOutcome := docusignFailure(createEnvelopeFromTemplateOperationID, sdkgo.FailureTransport, "an earlier attempt may have sent the DocuSign create request and its outcome is unknown")
	timeout, canRead := recoveryRequestTimeout(call)
	if canRead {
		response, err := client.listTaggedEnvelopes(call, session, marker, since, timeout)
		if client.isRetryableListing(response, err) {
			if hasCheckpoint {
				_ = call.Context.RecordHeartbeat(checkpoint) // A lost re-record leaves the prior value in place.
			}
			return client.retryListing(response, err), true
		}
		if err == nil && response.statusCode == http.StatusOK {
			matches, matchErr := findTaggedEnvelopes(response.body, marker)
			switch {
			case matchErr == nil && len(matches) == 1:
				return client.adoptRecoveredEnvelope(matches[0]), true
			case matchErr == nil && len(matches) > 1:
				return client.selectDuplicateEnvelopesUncertain(), true
			}
		}
	}
	if sends.canSendNow() {
		return sdkgo.MutationAttempt[CreatedEnvelope]{}, false
	}
	if hasCheckpoint {
		_ = call.Context.RecordHeartbeat(checkpoint) // A lost re-record leaves the prior value in place.
	}
	return sdkgo.NewMutationUncertain(CreatedEnvelope{}, unknownOutcome, client.receipt("", "")), true
}

// selectDuplicateEnvelopesUncertain refuses to pick among several tagged envelopes or to add another.
func (client *Client) selectDuplicateEnvelopesUncertain() sdkgo.MutationAttempt[CreatedEnvelope] {
	return sdkgo.NewMutationUncertain(CreatedEnvelope{}, docusignFailure(createEnvelopeFromTemplateOperationID, sdkgo.FailureConflict,
		"several DocuSign envelopes carry this Step's dexIdempotencyKey, so none was adopted and none was sent"), client.receipt("", ""))
}

// recoveryRequestTimeout bounds a read-back by the time left in the attempt.
func recoveryRequestTimeout(call sdkgo.Call) (time.Duration, bool) {
	timeout := docusignRequestTimeout
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
		timeout = min(timeout, time.Until(deadline)-minimumRecoveryRequestTime)
	}
	return timeout, timeout >= minimumRecoveryRequestTime
}

// listTaggedEnvelopes lists envelopes carrying marker, starting a clock-skew margin before since.
func (client *Client) listTaggedEnvelopes(
	call sdkgo.Call, session *docusignSession, marker string, since time.Time, timeout time.Duration,
) (docusignResponse, error) {
	query := url.Values{
		"from_date":    {since.Add(-recoveryLookbackMargin).UTC().Format(time.RFC3339)},
		"custom_field": {idempotencyCustomFieldName + "=" + marker},
		"include":      {"custom_fields"},
	}
	return client.sendAPIRequest(call, session, docusignRequest{method: http.MethodGet, path: "/envelopes", query: query, timeout: timeout}, nil)
}

// isRetryableListing reports a lost listing answer or one DocuSign rate limited or failed with a 5xx.
func (client *Client) isRetryableListing(response docusignResponse, err error) bool {
	if err != nil {
		return !errors.Is(err, errDocuSignResponseTooLarge)
	}
	return response.statusCode != http.StatusOK && client.classifyDocuSignFailure(createEnvelopeFromTemplateOperationID, response).isRetry
}

// retryListing retries a read-back that DocuSign could not answer.
func (client *Client) retryListing(response docusignResponse, err error) sdkgo.MutationAttempt[CreatedEnvelope] {
	if err != nil {
		return sdkgo.NewMutationRetry[CreatedEnvelope](docusignFailure(createEnvelopeFromTemplateOperationID, sdkgo.FailureAvailability, "DocuSign could not list envelopes to find the one an earlier attempt may have created"), 0)
	}
	outcome := client.classifyDocuSignFailure(createEnvelopeFromTemplateOperationID, response)
	return sdkgo.NewMutationRetry[CreatedEnvelope](outcome.failure, outcome.retryAfter)
}

// adoptRecoveredEnvelope selects created for the one envelope an earlier attempt created.
func (client *Client) adoptRecoveredEnvelope(recovered CreatedEnvelope) sdkgo.MutationAttempt[CreatedEnvelope] {
	recovered.WasRecovered = true
	return sdkgo.NewMutationBranch(CreateEnvelopeFromTemplateBranchCreated, recovered, nil, client.receipt(recovered.EnvelopeID, ""))
}

// buildCreateEnvelopeRequest validates input and adds the hidden marker custom field.
func buildCreateEnvelopeRequest(input CreateEnvelopeFromTemplateInput, marker string) (docusignCreateEnvelopeRequest, error) {
	if !isDocuSignGUID(input.TemplateID) {
		return docusignCreateEnvelopeRequest{}, errors.New("templateId must be a DocuSign template ID GUID, such as 00000000-0000-0000-0000-000000000000")
	}
	if len(input.TemplateRoles) == 0 || len(input.TemplateRoles) > maximumTemplateRoles {
		return docusignCreateEnvelopeRequest{}, fmt.Errorf("templateRoles must fill 1 to %d template roles", maximumTemplateRoles)
	}
	if !isBoundedText(input.EmailSubject, 0, maximumEmailSubjectLength) || !isBoundedText(input.EmailBlurb, 0, maximumEmailBlurbLength) {
		return docusignCreateEnvelopeRequest{}, fmt.Errorf("emailSubject must be at most %d characters and emailBlurb at most %d", maximumEmailSubjectLength, maximumEmailBlurbLength)
	}
	request := docusignCreateEnvelopeRequest{
		TemplateID: lowercaseGUID(input.TemplateID), Status: EnvelopeStatusSent,
		EmailSubject: input.EmailSubject, EmailBlurb: input.EmailBlurb,
	}
	if input.IsDraft {
		request.Status = EnvelopeStatusCreated
	}
	roleNames := map[string]bool{}
	for index, role := range input.TemplateRoles {
		if err := role.validate(); err != nil {
			return docusignCreateEnvelopeRequest{}, fmt.Errorf("templateRoles[%d]: %w", index, err)
		}
		if roleNames[strings.ToLower(role.RoleName)] {
			return docusignCreateEnvelopeRequest{}, fmt.Errorf("templateRoles[%d]: role %q is filled twice", index, role.RoleName)
		}
		roleNames[strings.ToLower(role.RoleName)] = true
		converted := docusignTemplateRole{RoleName: role.RoleName, Name: role.Name, Email: role.Email}
		if role.RoutingOrder > 0 {
			converted.RoutingOrder = strconv.Itoa(role.RoutingOrder)
		}
		request.TemplateRoles = append(request.TemplateRoles, converted)
	}
	if len(input.CustomFields) > maximumCustomFields {
		return docusignCreateEnvelopeRequest{}, fmt.Errorf("customFields holds at most %d fields", maximumCustomFields)
	}
	fieldNames := map[string]bool{strings.ToLower(idempotencyCustomFieldName): true}
	request.CustomFields.TextCustomFields = []docusignTextCustomField{{Name: idempotencyCustomFieldName, Value: marker, Show: "false", Required: "false"}}
	for index, field := range input.CustomFields {
		if !isBoundedText(field.Name, 1, maximumCustomFieldName) || !isBoundedText(field.Value, 0, maximumCustomFieldValue) {
			return docusignCreateEnvelopeRequest{}, fmt.Errorf("customFields[%d] needs a 1 to %d character name and a value of at most %d characters", index, maximumCustomFieldName, maximumCustomFieldValue)
		}
		if fieldNames[strings.ToLower(field.Name)] {
			return docusignCreateEnvelopeRequest{}, fmt.Errorf("customFields[%d]: name %q is reserved or repeated", index, field.Name)
		}
		fieldNames[strings.ToLower(field.Name)] = true
		request.CustomFields.TextCustomFields = append(request.CustomFields.TextCustomFields,
			docusignTextCustomField{Name: field.Name, Value: field.Value, Show: "false", Required: "false"})
	}
	return request, nil
}

func (role TemplateRole) validate() error {
	switch {
	case !isBoundedText(role.RoleName, 1, maximumRoleTextLength) || strings.TrimSpace(role.RoleName) == "":
		return fmt.Errorf("roleName must be 1 to %d characters", maximumRoleTextLength)
	case !isBoundedText(role.Name, 1, maximumRoleTextLength) || strings.TrimSpace(role.Name) == "":
		return fmt.Errorf("name must be 1 to %d characters", maximumRoleTextLength)
	case !isPlausibleEmail(role.Email):
		return errors.New("email must be one email address of at most 100 characters")
	case role.RoutingOrder < 0 || role.RoutingOrder > maximumRoutingOrder:
		return fmt.Errorf("routingOrder must be 1 to %d, or 0 to keep the template's order", maximumRoutingOrder)
	}
	return nil
}

// decodeCreatedEnvelope reads the 201 envelope summary and checks the status that was requested.
func decodeCreatedEnvelope(body []byte, isDraft bool) (CreatedEnvelope, error) {
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(body, &decoded); err != nil {
		return CreatedEnvelope{}, errDocuSignResponseMalformed
	}
	var envelopeID, status, statusDateTime string
	for field, target := range map[string]*string{"envelopeId": &envelopeID, "status": &status, docusignStatusDateTimeField: &statusDateTime} {
		if raw, isFound := decoded[field]; isFound && json.Unmarshal(raw, target) != nil {
			return CreatedEnvelope{}, errDocuSignResponseMalformed
		}
	}
	created := CreatedEnvelope{EnvelopeID: lowercaseGUID(envelopeID), Status: EnvelopeStatus(strings.ToLower(status))}
	expectedStatus := map[bool]EnvelopeStatus{true: EnvelopeStatusCreated, false: EnvelopeStatusSent}[isDraft]
	if !isDocuSignGUID(envelopeID) || created.Status != expectedStatus {
		return CreatedEnvelope{}, errDocuSignResponseMalformed
	}
	statusChangedAt, err := parseOptionalDocuSignTime(statusDateTime)
	if err != nil {
		return CreatedEnvelope{}, err
	}
	created.StatusChangedAt = statusChangedAt
	return created, nil
}

// findTaggedEnvelopes checks the marker field itself, in case DocuSign ignored the filter.
func findTaggedEnvelopes(body []byte, marker string) ([]CreatedEnvelope, error) {
	var decoded struct {
		Envelopes []docusignEnvelopeResource `json:"envelopes"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, errDocuSignResponseMalformed
	}
	var matches []CreatedEnvelope
	for _, resource := range decoded.Envelopes {
		envelope, err := resource.convert()
		if err != nil {
			return nil, err
		}
		for _, field := range envelope.CustomFields {
			if field.Name == idempotencyCustomFieldName && field.Value == marker {
				matches = append(matches, CreatedEnvelope{EnvelopeID: envelope.EnvelopeID, Status: envelope.Status, StatusChangedAt: envelope.StatusChangedAt})
				break
			}
		}
	}
	return matches, nil
}

// earlierCreateDispatch returns an earlier attempt's checkpoint; an unreadable one counts as dispatched.
func earlierCreateDispatch(call sdkgo.Call) (createDispatchCheckpoint, bool) {
	var checkpoint createDispatchCheckpoint
	isFound, err := call.Context.GetLastHeartbeatValue(&checkpoint)
	if err != nil || (isFound && checkpoint.IsDispatched && checkpoint.DispatchedAt.IsZero()) {
		return createDispatchCheckpoint{IsDispatched: true, DispatchedAt: call.Context.FirstAttemptAt()}, true
	}
	return checkpoint, isFound && checkpoint.IsDispatched
}

// releaseCreateDispatch clears this attempt's checkpoint after DocuSign provably created nothing,
// but keeps an earlier attempt's checkpoint, whose create may still exist.
func releaseCreateDispatch(call sdkgo.Call, checkpoint createDispatchCheckpoint, hasCheckpoint bool) {
	if hasCheckpoint {
		_ = call.Context.RecordHeartbeat(checkpoint) // A lost re-record leaves the prior value in place.
		return
	}
	clearCreateDispatch(call)
}

// clearCreateDispatch removes the checkpoint after DocuSign provably created nothing.
func clearCreateDispatch(call sdkgo.Call) {
	// A lost clear leaves the checkpoint set, so the next attempt reads back instead of creating twice.
	_ = call.Context.RecordHeartbeat(nil)
}

// isBoundedText reports valid UTF-8 text of minimum to maximum characters.
func isBoundedText(value string, minimum int, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum
}

// isPlausibleEmail accepts one address without spaces or angle brackets; DocuSign validates the rest.
func isPlausibleEmail(value string) bool {
	local, domain, hasAt := strings.Cut(value, "@")
	return hasAt && local != "" && strings.Contains(domain, ".") && !strings.Contains(domain, "@") &&
		len(value) <= maximumRoleTextLength && !strings.ContainsAny(value, " \t\r\n<>,;")
}
