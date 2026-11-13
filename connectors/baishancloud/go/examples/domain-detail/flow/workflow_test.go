// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package domaindetail

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/baishancloud/go"
	"github.com/superdurable/dex-connectors-library/connectors/baishancloud/go/baishancloudmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestDomainDetailsFlowIdentityAndMapping(t *testing.T) {
	input := baishancloud.DescribeDomainInput{Domain: "cdn.example.com"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(baishancloud.Connection{})))
	require.Equal(t, "StartDescribeDomain", dex.GetFinalStepType[baishancloud.DescribeDomainInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := baishancloudmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
