package v1alpha1

var (
	SKYCLUSTER_NAMESPACE            = "skycluster"
	SKYCLUSTER_API                  = "skycluster.io"
	SKYCLUSTER_COREGROUP            = "core." + SKYCLUSTER_API
	SKYCLUSTER_XRDsGROUP            = "xrds." + SKYCLUSTER_API
	SKYCLUSTER_VERSION              = "v1alpha1"
	SKYCLUSTER_MANAGEDBY_LABEL      = SKYCLUSTER_API + "/managed-by"
	SKYCLUSTER_MANAGEDBY_VALUE      = "skycluster"
	SKYCLUSTER_CONFIGTYPE_LABEL     = SKYCLUSTER_API + "/config-type"
	SKYCLUSTER_PROVIDERNAME_LABEL   = SKYCLUSTER_API + "/provider-name"
	SKYCLUSTER_PROVIDERREGION_LABEL = SKYCLUSTER_API + "/provider-region"
	SKYCLUSTER_PROVIDERZONE_LABEL   = SKYCLUSTER_API + "/provider-zone"
	SKYCLUSTER_PROVIDERTYPE_LABEL   = SKYCLUSTER_API + "/provider-type"
	SKYCLUSTER_PROJECTID_LABEL      = SKYCLUSTER_API + "/project-id"

	// SkyClusterConfigType values
	SKYCLUSTER_VPCCidrField_LABEL      = "vpcCidr"
	SKYCLUSTER_SubnetIndexField_LABEL  = "subnetIndex"
	SKYCLUSTER_ProvdiderMappings_LABEL = "provider-mappings"
	SKYCLUSTER_VSERVICES_LABEL         = "provider-vservices"
	SKYCLUSTER_FLAVORS                 = []string{
		"1vCPU-2GB", "1vCPU-4GB", "1vCPU-8GB",
		"2vCPU-4GB", "2vCPU-8GB", "2vCPU-16GB", "2vCPU-32GB",
		"4vCPU-8GB", "4vCPU-16GB", "4vCPU-32GB",
		"8vCPU-32GB",
		"12vCPU-32GB",
	}
)

// type SecGroupSpec struct {
// 	SecGroup []SecGroup `json:"secgroup"`
// }

// type SecGroup struct {
// 	TCPPorts []PortSpec `json:"tcpPorts,omitempty"`
// 	UDPPorts []PortSpec `json:"udpPorts,omitempty"`
// }

// type PortSpec struct {
// 	FromPort int `json:"fromPort"`
// 	ToPort   int `json:"toPort"`
// }

// type KeypairRefSpec struct {
// 	Name      string `json:"name,omitempty"`
// 	Namespace string `json:"namespace,omitempty"`
// }

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

// type ObjectDescriptor struct {
// 	Name      string `json:"name"`
// 	Namespace string `json:"namespace"`
// 	Group     string `json:"group"`
// 	Kind      string `json:"kind"`
// 	Version   string `json:"version,omitempty"`
// }
