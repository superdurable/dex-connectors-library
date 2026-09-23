package wrongtarget

import (
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

type wrongTarget struct{ dex.StepDefaultsNoWaitFor[int] }

func (wrongTarget) Execute(dex.Context, int) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

var _ = []connector.BranchTarget[connector.QueryStepOutput[string, string]]{
	connector.GoToBranch(connector.BranchID("succeeded"), wrongTarget{}),
}
