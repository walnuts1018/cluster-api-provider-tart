package update

// ChangeClassはactive configurationとdesired configurationの差分の分類である。
type ChangeClass string

const (
	// ChangeNoneは意味的な差分がないことを表す。
	ChangeNone ChangeClass = "None"
	// ChangeUpdatableはdata、identityを破壊しないため、policyに従ってin-placeで適用できる差分を表す。
	ChangeUpdatable ChangeClass = "Updatable"
	// ChangeReprovisionRequiredはdataまたはidentityを破壊するため、通常のupdateとして適用できない差分を表す。
	ChangeReprovisionRequired ChangeClass = "ReprovisionRequired"
	// ChangeInvariantConflictはprovider-owned invariantと競合する差分を表す。
	ChangeInvariantConflict ChangeClass = "InvariantConflict"
)

// Decisionはconfiguration差分に対する判定結果である。Reasonは利用者可視のためのメッセージとして英語で保持する。
type Decision struct {
	Class     ChangeClass
	ApplyMode ApplyMode
	Reason    string
}
