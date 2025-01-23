package v1alpha1

var (
	SkyClusterNamespace      = "skycluster"
	SkyClusterAPI            = "skycluster.io"
	SkyClusterCoreGroup      = "core." + SkyClusterAPI
	SkyClusterVersion        = "v1alpha1"
	SkyClusterManagedBy      = SkyClusterAPI + "/managed-by"
	SkyClusterManagedByValue = "skycluster"
	SkyClusterConfigType     = SkyClusterAPI + "/config-type"
	SkyClusterProviderName   = SkyClusterAPI + "/provider-name"
	SkyClusterProviderRegion = SkyClusterAPI + "/provider-region"
	SkyClusterProviderZone   = SkyClusterAPI + "/provider-zone"
	SkyClusterProviderType   = SkyClusterAPI + "/provider-type"
	SkyClusterProjectID      = SkyClusterAPI + "/project-id"
)

type SecGroupSpec struct {
	SecGroup []SecGroup `json:"secgroup"`
}

type SecGroup struct {
	TCPPorts []PortSpec `json:"tcpPorts,omitempty"`
	UDPPorts []PortSpec `json:"udpPorts,omitempty"`
}

type PortSpec struct {
	FromPort int `json:"fromPort"`
	ToPort   int `json:"toPort"`
}

type KeypairRefSpec struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type ProviderRefSpec struct {
	ProviderName        string `json:"providerName,omitempty"`
	ProviderRegion      string `json:"providerRegion,omitempty"`
	ProviderRegionAlias string `json:"providerRegionAlias,omitempty"`
	ProviderType        string `json:"providerType,omitempty"`
	ProviderZone        string `json:"providerZone,omitempty"`
}

type MonitoringMetricSpec struct {
	Type     string       `json:"type"`
	Resource ResourceSpec `json:"resource,omitempty"`
	Metric   MetricSpec   `json:"metric,omitempty"`
}

type ResourceSpec struct {
	Name string `json:"name"`
}

type MetricSpec struct {
	Endpoint string `json:"endpoint"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type ObjectSpec struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type ObjectGroupSpec struct {
	Group   string       `json:"group"`
	Kind    string       `json:"kind"`
	Version string       `json:"version,omitempty"`
	Items   []ObjectSpec `json:"items"`
}
