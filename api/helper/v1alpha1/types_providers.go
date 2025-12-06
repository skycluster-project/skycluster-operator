package v1alpha1

type ProviderRefSpec struct {
	Name        string `json:"name,omitempty"`
	Platform    string `json:"platform,omitempty"`
	Type        string `json:"type,omitempty"`
	Region      string `json:"region,omitempty"`
	RegionAlias string `json:"regionAlias,omitempty"`
	Zone        string `json:"zone,omitempty"`
	ConfigName  string `json:"configName,omitempty"`
}
