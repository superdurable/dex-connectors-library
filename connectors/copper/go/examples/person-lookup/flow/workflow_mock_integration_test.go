//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/connectors/copper/go/coppermock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPersonLookupUsesDefaultMockWithRealDex(t *testing.T) {
	mock := coppermock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	searchFlow := NewPeopleSearchFlow(mock.Connection())
	companySearchFlow := NewCompaniesSearchFlow(mock.Connection())
	leadSearchFlow := NewLeadsSearchFlow(mock.Connection())
	leadCreationFlow := NewLeadCreationFlow(mock.Connection())
	opportunitySearchFlow := NewOpportunitiesSearchFlow(mock.Connection())
	opportunityCreationFlow := NewOpportunityCreationFlow(mock.Connection())
	projectSearchFlow := NewProjectsSearchFlow(mock.Connection())
	projectCreationFlow := NewProjectCreationFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, searchFlow, companySearchFlow, leadSearchFlow, leadCreationFlow, opportunitySearchFlow, opportunityCreationFlow, projectSearchFlow, projectCreationFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "copper-personlookup-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, copper.GetPersonInput{PersonID: "26443553"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output copper.PersonMetadata
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, int64(26443553), output.ID)
	require.Len(t, mock.GetPerson().Calls(), 1)
	searchFlowID := "copper-people-search-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, searchFlow, searchFlowID, copper.ListPeopleInput{PageNumber: 1, PageSize: 25}, dex.StartFlowOptions{})
	require.NoError(t, err)
	searchResult, err := client.WaitForFlow(ctx, searchFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, searchResult.Status, searchResult.ErrorMessage)
	var peoplePage copper.PeoplePage
	require.NoError(t, searchResult.DecodeSingleOutput(&peoplePage))
	require.Len(t, peoplePage.People, 1)
	require.Len(t, mock.ListPeople().Calls(), 1)
	companyFlowID := "copper-company-search-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, companySearchFlow, companyFlowID, copper.ListCompaniesInput{PageNumber: 1, PageSize: 25}, dex.StartFlowOptions{})
	require.NoError(t, err)
	companyResult, err := client.WaitForFlow(ctx, companyFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, companyResult.Status, companyResult.ErrorMessage)
	var companyPage copper.CompaniesPage
	require.NoError(t, companyResult.DecodeSingleOutput(&companyPage))
	require.Len(t, companyPage.Companies, 1)
	require.Len(t, mock.ListCompanies().Calls(), 1)
	leadFlowID := "copper-lead-search-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, leadSearchFlow, leadFlowID, copper.ListLeadsInput{PageNumber: 1, PageSize: 25}, dex.StartFlowOptions{})
	require.NoError(t, err)
	leadResult, err := client.WaitForFlow(ctx, leadFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, leadResult.Status, leadResult.ErrorMessage)
	var leadPage copper.LeadsPage
	require.NoError(t, leadResult.DecodeSingleOutput(&leadPage))
	require.Len(t, leadPage.Leads, 1)
	require.Len(t, mock.ListLeads().Calls(), 1)
	creationFlowID := "copper-lead-creation-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, leadCreationFlow, creationFlowID, copper.CreateLeadInput{Name: "Sample Lead", Email: "sample@example.com"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	creationResult, err := client.WaitForFlow(ctx, creationFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, creationResult.Status, creationResult.ErrorMessage)
	var createdLead copper.LeadMetadata
	require.NoError(t, creationResult.DecodeSingleOutput(&createdLead))
	require.Equal(t, int64(8894157), createdLead.ID)
	require.Len(t, mock.CreateLead().Calls(), 1)
	opportunityFlowID := "copper-opportunity-search-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, opportunitySearchFlow, opportunityFlowID, copper.ListOpportunitiesInput{PageNumber: 1, PageSize: 25}, dex.StartFlowOptions{})
	require.NoError(t, err)
	opportunityResult, err := client.WaitForFlow(ctx, opportunityFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, opportunityResult.Status, opportunityResult.ErrorMessage)
	var opportunityPage copper.OpportunitiesPage
	require.NoError(t, opportunityResult.DecodeSingleOutput(&opportunityPage))
	require.Len(t, opportunityPage.Opportunities, 1)
	require.Len(t, mock.ListOpportunities().Calls(), 1)
	opportunityCreationFlowID := "copper-opportunity-creation-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, opportunityCreationFlow, opportunityCreationFlowID, copper.CreateOpportunityInput{Name: "Sample Opportunity"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	opportunityCreationResult, err := client.WaitForFlow(ctx, opportunityCreationFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, opportunityCreationResult.Status, opportunityCreationResult.ErrorMessage)
	var createdOpportunity copper.OpportunityMetadata
	require.NoError(t, opportunityCreationResult.DecodeSingleOutput(&createdOpportunity))
	require.Equal(t, int64(4956209), createdOpportunity.ID)
	require.Len(t, mock.CreateOpportunity().Calls(), 1)
	projectFlowID := "copper-project-search-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, projectSearchFlow, projectFlowID, copper.ListProjectsInput{PageNumber: 1, PageSize: 25}, dex.StartFlowOptions{})
	require.NoError(t, err)
	projectResult, err := client.WaitForFlow(ctx, projectFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, projectResult.Status, projectResult.ErrorMessage)
	var projectPage copper.ProjectsPage
	require.NoError(t, projectResult.DecodeSingleOutput(&projectPage))
	require.Len(t, projectPage.Projects, 1)
	require.Len(t, mock.ListProjects().Calls(), 1)
	projectCreationFlowID := "copper-project-creation-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, projectCreationFlow, projectCreationFlowID, copper.CreateProjectInput{Name: "Sample Project"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	projectCreationResult, err := client.WaitForFlow(ctx, projectCreationFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, projectCreationResult.Status, projectCreationResult.ErrorMessage)
	var createdProject copper.ProjectMetadata
	require.NoError(t, projectCreationResult.DecodeSingleOutput(&createdProject))
	require.Equal(t, int64(208105), createdProject.ID)
	require.Len(t, mock.CreateProject().Calls(), 1)
}
