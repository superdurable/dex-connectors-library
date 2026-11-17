// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const createDisputeFileEvidenceGraphQL = "mutation CreateDisputeFileEvidence($input: CreateDisputeFileEvidenceInput!) { createDisputeFileEvidence(input: $input) { dispute { id } evidence { id category } } }"
const maximumDisputeEvidenceBytes = 4 * 1024 * 1024

// CreateDisputeFileEvidenceInput references an immutable file on every Worker.
type CreateDisputeFileEvidenceInput struct {
	// DisputeID is the GraphQL dispute ID.
	DisputeID string `json:"disputeId"`
	// Category is a DisputeFileEvidenceCategory enum.
	Category string `json:"category"`
	// FileName is a basename in the configured evidence directory.
	FileName string `json:"fileName"`
	// ExpectedSHA256 is the lowercase hex digest of the complete file.
	ExpectedSHA256 string `json:"expectedSha256"`
}

// CreateDisputeFileEvidenceOperation uploads a worker-mounted file.
type CreateDisputeFileEvidenceOperation struct{ client *Client }

// CreateDisputeFileEvidence binds the Mutation.
func (client *Client) CreateDisputeFileEvidence() CreateDisputeFileEvidenceOperation {
	return CreateDisputeFileEvidenceOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateDisputeFileEvidenceOperation) Definition() sdkgo.MutationDefinition {
	return CreateDisputeFileEvidenceDefinition
}

// IdempotencyKey records Dex identity; Braintree does not deduplicate uploads.
func (CreateDisputeFileEvidenceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDisputeFileEvidenceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke uploads one file with bounded resends after an unknown outcome.
func (operation CreateDisputeFileEvidenceOperation) Invoke(call sdkgo.Call, input CreateDisputeFileEvidenceInput) sdkgo.MutationAttempt[DisputeEvidenceProfile] {
	const operationName = "createDisputeFileEvidence"
	if !validGraphQLID(input.DisputeID) || !validEvidenceCategory(input.Category) || !validEvidenceFileName(input.FileName) || !validSHA256Hex(input.ExpectedSHA256) {
		return braintreeMutationBranch[DisputeEvidenceProfile](operationName, CreateDisputeFileEvidenceBranchDefect, sdkgo.FailureValidation, "dispute file evidence input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateDisputeFileEvidenceDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return braintreeMutationBranch[DisputeEvidenceProfile](operationName, CreateDisputeFileEvidenceBranchDefect, sdkgo.FailureLocalDefect, "file evidence checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return braintreeMutationUncertain[DisputeEvidenceProfile](operationName, "file evidence checkpoint cannot be trusted")
	}
	if decision.Kind != singledispatch.SendAllowed {
		return braintreeMutationUncertain[DisputeEvidenceProfile](operationName, "file evidence outcome cannot be confirmed after the send cap")
	}
	fileBytes, contentType, err := operation.client.readDisputeEvidenceFile(input)
	if err != nil {
		if decision.MaximumPriorSends > 0 {
			return braintreeMutationUncertain[DisputeEvidenceProfile](operationName, "file evidence changed after a possible send")
		}
		return braintreeMutationBranch[DisputeEvidenceProfile](operationName, CreateDisputeFileEvidenceBranchDefect, sdkgo.FailureValidation, "file evidence is unavailable or invalid")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.uploadDisputeEvidence(call, requestContext, input, fileBytes, contentType)
		return nil
	})
	if err != nil {
		return braintreeMutationRetry[DisputeEvidenceProfile](operationName, sdkgo.FailureAvailability, "file evidence dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return braintreeMutationRetry[DisputeEvidenceProfile](operationName, sdkgo.FailureAvailability, "file evidence resend checkpoint is unavailable", 0)
		}
	}
	if attempt, handled := classifyBraintreeMutationResponse[DisputeEvidenceProfile](response, operationName, CreateDisputeFileEvidenceBranchNotFound, CreateDisputeFileEvidenceBranchProviderRejected, "", CreateDisputeFileEvidenceBranchDefect, decision.MaximumPriorSends > 0); handled {
		return attempt
	}
	var wire struct {
		Data struct {
			CreateDisputeFileEvidence struct {
				Dispute struct {
					ID string `json:"id"`
				} `json:"dispute"`
				Evidence DisputeEvidenceProfile `json:"evidence"`
			} `json:"createDisputeFileEvidence"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data.CreateDisputeFileEvidence.Dispute.ID != input.DisputeID || !validGraphQLID(wire.Data.CreateDisputeFileEvidence.Evidence.ID) || wire.Data.CreateDisputeFileEvidence.Evidence.Category != input.Category {
		return braintreeMutationRetry[DisputeEvidenceProfile](operationName, sdkgo.FailureProtocol, "Braintree file evidence response is uncertain", 0)
	}
	return sdkgo.NewMutationBranch(CreateDisputeFileEvidenceBranchCreated, wire.Data.CreateDisputeFileEvidence.Evidence, nil, sdkgo.Receipt{})
}

func validEvidenceFileName(fileName string) bool {
	if len(fileName) == 0 || len(fileName) > 255 || fileName == "." || fileName == ".." {
		return false
	}
	for _, character := range fileName {
		if character != '.' && character != '-' && character != '_' && (character < '0' || character > '9') && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}

func validSHA256Hex(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for _, character := range digest {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func (client *Client) readDisputeEvidenceFile(input CreateDisputeFileEvidenceInput) ([]byte, string, error) {
	if client.evidenceDirectory == "" {
		return nil, "", errors.New("evidence directory is disabled")
	}
	root, err := filepath.EvalSymlinks(client.evidenceDirectory)
	if err != nil {
		return nil, "", err
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, input.FileName))
	if err != nil {
		return nil, "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, "", errors.New("evidence file escapes configured directory")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = file.Close() }()
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() <= 0 || fileInfo.Size() > maximumDisputeEvidenceBytes {
		return nil, "", errors.New("evidence file size or type is invalid")
	}
	fileBytes, err := io.ReadAll(io.LimitReader(file, maximumDisputeEvidenceBytes+1))
	if err != nil || len(fileBytes) == 0 || len(fileBytes) > maximumDisputeEvidenceBytes {
		return nil, "", errors.New("evidence file read failed")
	}
	digest := sha256.Sum256(fileBytes)
	if hex.EncodeToString(digest[:]) != input.ExpectedSHA256 {
		return nil, "", errors.New("evidence file digest changed")
	}
	contentType := http.DetectContentType(fileBytes)
	switch contentType {
	case "image/png", "image/jpeg", "application/pdf":
		return fileBytes, contentType, nil
	default:
		return nil, "", errors.New("evidence file format is unsupported")
	}
}

func (client *Client) uploadDisputeEvidence(call sdkgo.Call, requestContext context.Context, input CreateDisputeFileEvidenceInput, fileBytes []byte, contentType string) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	publicKey, privateKey := credentials.PublicKey, credentials.PrivateKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(publicKey) || strings.Contains(publicKey, ":") || !providerhttp.IsHeaderSafeCredential(privateKey) {
		return providerResponse{err: errCredential}
	}
	operations, err := json.Marshal(map[string]any{"query": createDisputeFileEvidenceGraphQL, "variables": map[string]any{"input": map[string]any{"clientMutationId": string(call.ID), "disputeId": input.DisputeID, "category": input.Category}, "file": nil}})
	if err != nil {
		return providerResponse{err: errRequest}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	operationHeader := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="operations"`}, "Content-Type": {"application/json"}}
	operationPart, err := writer.CreatePart(operationHeader)
	if err == nil {
		_, err = operationPart.Write(operations)
	}
	if err == nil {
		err = writer.WriteField("map", `{"file_to_upload":"variables.file"}`)
	}
	if err == nil {
		fileHeader := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file_to_upload"; filename="` + input.FileName + `"`}, "Content-Type": {contentType}}
		var filePart io.Writer
		filePart, err = writer.CreatePart(fileHeader)
		if err == nil {
			_, err = filePart.Write(fileBytes)
		}
	}
	if err == nil {
		err = writer.Close()
	}
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, client.baseURL+"/graphql", &body)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Braintree-Version", client.apiVersion)
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(publicKey+":"+privateKey)))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: responseBody, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
