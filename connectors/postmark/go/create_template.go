// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateTemplateOperation creates one standard template with a unique call-derived alias.
type CreateTemplateOperation struct{ client *Client }

// CreateTemplate binds template creation to this client.
func (client *Client) CreateTemplate() CreateTemplateOperation {
	return CreateTemplateOperation{client}
}

// Definition returns the generated createTemplate contract.
func (CreateTemplateOperation) Definition() sdkgo.MutationDefinition { return CreateTemplateDefinition }

// IdempotencyKey derives a unique template alias from the durable Call ID.
func (CreateTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke checks the call-specific alias before any repeat and fences each POST.
func (operation CreateTemplateOperation) Invoke(call sdkgo.Call, input CreateTemplateInput) sdkgo.MutationAttempt[Template] {
	if !validTemplateContent(input.Name, input.Subject, input.TextBody, input.HtmlBody) {
		return mutationFailure[Template]("createTemplate", CreateTemplateBranchDefect, sdkgo.FailureValidation, "template name, subject, and a body are required")
	}
	alias := "dex-" + string(call.IdempotencyKey)
	fence, err := singledispatch.NewFence(call, CreateTemplateDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationFailure[Template]("createTemplate", CreateTemplateBranchDefect, sdkgo.FailureLocalDefect, "template dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(Template{}, newFailure("createTemplate", sdkgo.FailureTransport, "template dispatch checkpoint cannot be read"), sdkgo.Receipt{})
	}
	if decision.Kind != singledispatch.SendAllowed {
		template, found, readErr := operation.client.findCreatedTemplate(call, alias, input)
		if readErr != nil {
			return sdkgo.NewMutationRetry[Template](newFailure("createTemplate", sdkgo.FailureAvailability, "template could not be checked"), 0)
		}
		if found {
			return sdkgo.NewMutationBranch(CreateTemplateBranchCreated, template, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(Template{}, newFailure("createTemplate", sdkgo.FailureTransport, "template may have been created but could not be confirmed"), sdkgo.Receipt{})
		}
	}
	payload, err := json.Marshal(map[string]string{"Name": input.Name, "Subject": input.Subject, "TextBody": input.TextBody, "HtmlBody": input.HtmlBody, "Alias": alias})
	if err != nil {
		return mutationFailure[Template]("createTemplate", CreateTemplateBranchDefect, sdkgo.FailureLocalDefect, "template request cannot be encoded")
	}
	request, err := operation.client.newRequest(call, http.MethodPost, "/templates", payload)
	if err != nil {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(Template{}, newFailure("createTemplate", sdkgo.FailureAuthentication, "an earlier template create may have succeeded"), sdkgo.Receipt{})
		}
		return mutationFailure[Template]("createTemplate", CreateTemplateBranchDefect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendErr error
		response, sendErr = operation.client.httpClient.Do(request.WithContext(requestContext))
		return sendErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[Template](newFailure("createTemplate", sdkgo.FailureAvailability, "template dispatch did not start"), 0)
	}
	if err != nil {
		return sdkgo.NewMutationRetry[Template](newFailure("createTemplate", sdkgo.FailureTransport, "template create outcome is unknown"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[Template](newFailure("createTemplate", sdkgo.FailureAvailability, "template retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[Template](newFailure("createTemplate", kind, "Postmark temporarily refused template creation"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		return sdkgo.NewMutationRetry[Template](newFailure("createTemplate", sdkgo.FailureAvailability, "template create outcome is unknown"), 0)
	}
	if response.StatusCode != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(Template{}, newFailure("createTemplate", sdkgo.FailureProviderRejection, "a prior template create may have succeeded before this refusal"), sdkgo.Receipt{})
		}
		return mutationFailure[Template]("createTemplate", CreateTemplateBranchProviderRejected, sdkgo.FailureProviderRejection, "Postmark refused template creation")
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return sdkgo.NewMutationRetry[Template](newFailure("createTemplate", sdkgo.FailureTransport, "template acceptance response was interrupted"), 0)
	}
	template, err := decodeTemplate(content)
	if err != nil || template.Alias != alias {
		return sdkgo.NewMutationRetry[Template](newFailure("createTemplate", sdkgo.FailureProtocol, "template acceptance response was incomplete"), 0)
	}
	if err := fence.RecordProviderID(strconv.FormatInt(template.TemplateID, 10)); err != nil {
		return sdkgo.NewMutationRetry[Template](newFailure("createTemplate", sdkgo.FailureAvailability, "accepted template checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(CreateTemplateBranchCreated, template, nil, sdkgo.Receipt{})
}

func (client *Client) findCreatedTemplate(call sdkgo.Call, alias string, input CreateTemplateInput) (Template, bool, error) {
	request, err := client.newRequest(call, http.MethodGet, "/templates/"+url.PathEscape(alias), nil)
	if err != nil {
		return Template{}, false, err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return Template{}, false, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusUnprocessableEntity && response.Header.Get("X-PM-ApiErrorCode") == "1101" {
		return Template{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return Template{}, false, fmt.Errorf("template lookup returned HTTP %d", response.StatusCode)
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return Template{}, false, err
	}
	template, err := decodeTemplate(content)
	if err != nil {
		return Template{}, false, err
	}
	if template.Alias != alias || template.Name != input.Name || template.Subject != input.Subject || template.TextBody != input.TextBody || template.HtmlBody != input.HtmlBody {
		return Template{}, false, fmt.Errorf("call-specific template alias has unexpected content")
	}
	return template, true, nil
}
