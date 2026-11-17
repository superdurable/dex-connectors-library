// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package databricks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

var clusterIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// CreateJobInput creates one notebook job on an existing cluster.
type CreateJobInput struct {
	// Name is the visible job name, 1 to 255 Unicode characters.
	Name string `json:"name"`
	// NotebookPath is the absolute workspace notebook path, starting with /.
	NotebookPath string `json:"notebookPath"`
	// ExistingClusterID is the cluster identifier copied from Compute in the workspace.
	ExistingClusterID string `json:"existingClusterId"`
}

// CreateJobOperation creates a saved job with bounded repeat attempts.
type CreateJobOperation struct{ client *Client }

type createJobSettings struct {
	Name  string          `json:"name"`
	Tasks []createJobTask `json:"tasks"`
}

type createJobTask struct {
	TaskKey           string            `json:"task_key"`
	ExistingClusterID string            `json:"existing_cluster_id"`
	NotebookTask      createJobNotebook `json:"notebook_task"`
}

type createJobNotebook struct {
	NotebookPath string `json:"notebook_path"`
}

// CreateJob binds the mutation to this client.
func (client *Client) CreateJob() CreateJobOperation { return CreateJobOperation{client} }

// Definition returns the generated mutation definition.
func (CreateJobOperation) Definition() sdkgo.MutationDefinition { return CreateJobDefinition }

// IdempotencyKey correlates the call; Databricks does not accept it for job creation.
func (CreateJobOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateJobInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one job, consulting the fence before a bounded resend.
func (operation CreateJobOperation) Invoke(call sdkgo.Call, input CreateJobInput) sdkgo.MutationAttempt[JobSummary] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || !validListText(input.Name, 255) ||
		!strings.HasPrefix(input.NotebookPath, "/") || !validListText(input.NotebookPath, 4096) ||
		!clusterIDPattern.MatchString(input.ExistingClusterID) {
		return mutationBranch[JobSummary](CreateJobBranchDefect, "createJob", sdkgo.FailureValidation, "valid name, absolute notebook path, and cluster ID are required")
	}
	fence, err := singledispatch.NewFence(call, CreateJobDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationBranch[JobSummary](CreateJobBranchDefect, "createJob", sdkgo.FailureLocalDefect, "job creation checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return operation.uncertainCreate(call, "job creation checkpoint is unreadable")
	}
	if decision.Marker.ProviderID != "" {
		return operation.readCreatedJob(call, input, decision.Marker.ProviderID)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return operation.uncertainCreate(call, "earlier job creation may have applied without a confirmed ID")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		if decision.Kind == singledispatch.SendAllowed {
			_ = fence.RecordNotSent()
		}
		return mutationBranch[JobSummary](CreateJobBranchDefect, "createJob", sdkgo.FailureAuthentication, "Databricks credentials are unavailable or invalid")
	}
	body, err := json.Marshal(createJobSettings{Name: input.Name, Tasks: []createJobTask{{
		TaskKey: "main", ExistingClusterID: input.ExistingClusterID,
		NotebookTask: createJobNotebook{NotebookPath: input.NotebookPath},
	}}})
	if err != nil {
		return mutationBranch[JobSummary](CreateJobBranchDefect, "createJob", sdkgo.FailureLocalDefect, "job settings could not be encoded")
	}
	send := &createJobSend{client: operation.client, call: call, body: body}
	if err := fence.Dispatch(send.execute); err != nil {
		return mutationRetry[JobSummary]("createJob", sdkgo.FailureAvailability, "job creation was not dispatched after checkpoint", 0)
	}
	response := send.response
	switch {
	case errors.Is(response.err, errCredential):
		_ = fence.RecordCanResend()
		return mutationRetry[JobSummary]("createJob", sdkgo.FailureAuthentication, "Databricks credentials became unavailable before dispatch", 0)
	case response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable:
		_ = fence.RecordCanResend()
		return mutationRetry[JobSummary]("createJob", sdkgo.FailureAvailability, "Databricks temporarily refused job creation", response.retryAfter)
	case response.err != nil || response.status >= 500:
		return operation.retryOrUncertain(call, decision, response.retryAfter, "Databricks job creation outcome is unknown")
	case response.status != http.StatusOK:
		if decision.Kind != singledispatch.SendAllowed {
			return operation.retryOrUncertain(call, decision, 0, "an earlier job creation may have applied before this refusal")
		}
		return mutationBranch[JobSummary](CreateJobBranchProviderRejected, "createJob", sdkgo.FailureProviderRejection, "Databricks rejected job creation with HTTP "+strconv.Itoa(response.status))
	}
	var answer struct {
		JobID int64 `json:"job_id"`
	}
	if json.Unmarshal(response.body, &answer) != nil || answer.JobID <= 0 {
		return operation.retryOrUncertain(call, decision, 0, "Databricks accepted job creation without a usable job ID")
	}
	jobID := strconv.FormatInt(answer.JobID, 10)
	_ = fence.RecordProviderID(jobID)
	return sdkgo.NewMutationBranch(CreateJobBranchCreated, JobSummary{ID: jobID, Name: input.Name}, nil, jobReceipt(call, jobID))
}

func (operation CreateJobOperation) retryOrUncertain(call sdkgo.Call, decision singledispatch.Decision, retryAfter time.Duration, message string) sdkgo.MutationAttempt[JobSummary] {
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return operation.uncertainCreate(call, message)
	}
	return mutationRetry[JobSummary]("createJob", sdkgo.FailureAvailability, message, retryAfter)
}

func (operation CreateJobOperation) readCreatedJob(call sdkgo.Call, input CreateJobInput, jobID string) sdkgo.MutationAttempt[JobSummary] {
	response := operation.client.get(call, "/api/2.2/jobs/get?job_id="+url.QueryEscape(jobID))
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return mutationRetry[JobSummary]("createJob", sdkgo.FailureAvailability, "created job read-back is unavailable", response.retryAfter)
	}
	var answer struct {
		JobID    int64 `json:"job_id"`
		Settings struct {
			Name string `json:"name"`
		} `json:"settings"`
	}
	if response.status != http.StatusOK || json.Unmarshal(response.body, &answer) != nil || strconv.FormatInt(answer.JobID, 10) != jobID || answer.Settings.Name != input.Name {
		return operation.uncertainCreate(call, "created job could not be confirmed by ID and name")
	}
	return sdkgo.NewMutationBranch(CreateJobBranchCreated, JobSummary{ID: jobID, Name: input.Name}, nil, jobReceipt(call, jobID))
}

func (CreateJobOperation) uncertainCreate(call sdkgo.Call, message string) sdkgo.MutationAttempt[JobSummary] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "databricks", Operation: "createJob", Message: message}
	return sdkgo.NewMutationUncertain(JobSummary{}, failure, jobReceipt(call, ""))
}

type createJobSend struct {
	client   *Client
	call     sdkgo.Call
	body     []byte
	response providerResponse
}

func (send *createJobSend) execute(ctx context.Context) error {
	send.response = send.client.exchangeWithContext(ctx, send.call, http.MethodPost, "/api/2.2/jobs/create", send.body)
	return nil
}
