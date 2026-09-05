package v1alpha1

// ClusterIDはKubernetes APIに保存するCluster識別子である。
// ドメイン層のUUID型とは境界を分け、APIのJSON表現を文字列として保持する。
type ClusterID string

// IsZeroはClusterIDが未設定かを返す。
func (id ClusterID) IsZero() bool {
	return id == ""
}

// StringはAPI上のClusterID文字列を返す。
func (id ClusterID) String() string {
	return string(id)
}

// HostIDはKubernetes APIに保存するHost識別子である。
// ドメイン層のUUID型とは境界を分け、APIのJSON表現を文字列として保持する。
type HostID string

// IsZeroはHostIDが未設定かを返す。
func (id HostID) IsZero() bool {
	return id == ""
}

// StringはAPI上のHostID文字列を返す。
func (id HostID) String() string {
	return string(id)
}
