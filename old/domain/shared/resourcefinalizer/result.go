package resourcefinalizer

type Result interface {
	isResult()
}

type ResultUnchanged struct{}
type ResultPatched struct{}

func (ResultUnchanged) isResult() {}
func (ResultPatched) isResult()   {}
