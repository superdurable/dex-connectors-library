// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package templatelist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/go"
	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/go/sendgridmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTemplateFlowIdentityAndMapping(t *testing.T) {
	input := sendgrid.ListTemplatesInput{PageSize: 100}
	require.Equal(t, input, MapToTemplateOperationInput(input))
	require.Equal(t, TemplateFlowType, dex.GetFinalFlowType(NewTemplateFlow(sendgrid.Connection{})))
	require.Equal(t, "StartListTemplates", dex.GetFinalStepType[sendgrid.ListTemplatesInput](startTemplateRequest{}))
	wait, err := startTemplateRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := sendgridmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewTemplateFlow(mock.Connection())})
	require.NoError(t, err)
}
