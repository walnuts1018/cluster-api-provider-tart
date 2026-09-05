package allocation

import "errors"

var ErrConflict = errors.New("TartHost allocation conflict")

type ReferenceResult string

const (
	ReferenceMissing    ReferenceResult = "Missing"
	ReferenceConsistent ReferenceResult = "Consistent"
	ReferenceRepaired   ReferenceResult = "Repaired"
)
