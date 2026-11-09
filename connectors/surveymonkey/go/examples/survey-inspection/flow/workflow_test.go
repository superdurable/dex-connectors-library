// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package surveyinspection

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/surveymonkey/go"
	"github.com/superdurable/dex-connectors-library/connectors/surveymonkey/go/surveymonkeymock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestSurveyInspectionFlowIdentityAndMapping(t *testing.T) {
	input := surveymonkey.ListSurveysInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(surveymonkey.Connection{})))
	require.Equal(t, "StartListSurveys", dex.GetFinalStepType[surveymonkey.ListSurveysInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := surveymonkeymock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
