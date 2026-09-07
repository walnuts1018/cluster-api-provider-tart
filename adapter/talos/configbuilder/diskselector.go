package configbuilder

import (
	googlecel "github.com/google/cel-go/cel"
	"github.com/siderolabs/talos/pkg/machinery/cel"

	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
)

// diskExpressionは、diskを一意に識別する最も具体的なselectorをcel.Expressionとして返す。
// providerがinstall targetへ強制するselectorは、常にこの最も具体的な表現を採用する。
// selector文字列の構築自体はdomain/bootstrapへ委譲し、この関数はsiderolabs machineryの
// cel.Expressionへの変換だけを担う。
func diskExpression(disk domainbootstrap.DiskIdentity, env *googlecel.Env) (cel.Expression, error) {
	selectors := domainbootstrap.DiskSelectorsFor(disk)
	if len(selectors) == 0 {
		return cel.Expression{}, domainbootstrap.ErrDiskSelectionUnavailable
	}
	expression, err := cel.ParseBooleanExpression(selectors[0].Expression, env)
	if err != nil {
		return cel.Expression{}, err
	}
	return expression, nil
}
