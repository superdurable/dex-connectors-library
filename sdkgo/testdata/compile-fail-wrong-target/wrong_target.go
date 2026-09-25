// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wrongtarget

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type wrongTarget struct{ dex.StepDefaultsNoWaitFor[int] }

func (wrongTarget) Execute(dex.Context, int) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

var _ = []sdkgo.BranchTarget[sdkgo.QueryResult[string]]{
	sdkgo.GoToBranch(sdkgo.BranchID("succeeded"), wrongTarget{}),
}
