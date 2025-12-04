package v1alpha1

import (
	"encoding/json"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	SKYCLUSTER_NAMESPACE           = "skycluster-system"
	SKYCLUSTER_API                 = "skycluster.io"
	SKYCLUSTER_COREGROUP           = "core." + SKYCLUSTER_API
	SKYCLUSTER_XRDsGROUP           = "xrds." + SKYCLUSTER_API
	SKYCLUSTER_VERSION             = "v1alpha1"
	SKYCLUSTER_MANAGEDBY_LABEL     = SKYCLUSTER_API + "/managed-by"
	SKYCLUSTER_MANAGEDBY_VALUE     = "skycluster"
	SKYCLUSTER_PAUSE_LABEL         = SKYCLUSTER_API + "/pause"
	SKYCLUSTER_ORIGINAL_NAME_LABEL = SKYCLUSTER_API + "/original-name"
	SKYCLUSTER_PROJECTID_LABEL     = SKYCLUSTER_API + "/project-id"

	SKYCLUSTER_CONFIGTYPE_LABEL = SKYCLUSTER_API + "/config-type"
	SKYCLUSTER_SVCTYPE_LABEL    = SKYCLUSTER_API + "/service-type"

	SKYCLUSTER_PROVIDERNAME_LABEL        = SKYCLUSTER_API + "/provider-name"
	SKYCLUSTER_PROVIDERREGIONALIAS_LABEL = SKYCLUSTER_API + "/provider-region-alias"
	SKYCLUSTER_PROVIDERREGION_LABEL      = SKYCLUSTER_API + "/provider-region"
	SKYCLUSTER_PROVIDERZONE_LABEL        = SKYCLUSTER_API + "/provider-zone"
	SKYCLUSTER_PROVIDERTYPE_LABEL        = SKYCLUSTER_API + "/provider-type"
	SKYCLUSTER_PROVIDERID_LABEL          = SKYCLUSTER_API + "/provider-identifier"

	// SkyClusterConfigType values
	SKYCLUSTER_VPCCidrField_LABEL      = "vpcCidr"
	SKYCLUSTER_SubnetIndexField_LABEL  = "subnetIndex"
	SKYCLUSTER_ProvdiderMappings_LABEL = "provider-mappings"
	SKYCLUSTER_VSERVICES_LABEL         = "provider-vservices"
)

type LocationConstraint struct {
	// Required specifies location sets that must be satisfied (AND logic).
	Required []LocationCondition `json:"required,omitempty"`
	// Permitted specifies locations that are allowed (OR logic).
	Permitted []ProviderRefSpec `json:"permitted,omitempty"`
}

// LocationCondition represents a group of alternative location rules (OR logic).
type LocationCondition struct {
	AnyOf []ProviderRefSpec `json:"anyOf,omitempty"`
}

type VirtualService struct {
	// Name specifies the name of virtual service, if applicable
	Name string       `json:"name,omitempty"`
	// Spec specifies the spec of virtual service in JSON format, if applicable
	Spec string  	 		`json:"spec,omitempty"`
	// +kubebuilder:validation:Enum=ComputeProfile
	Kind string       `json:"kind,omitempty"`
	metav1.TypeMeta `json:",inline"`
}

type DeployMapEdge struct {
	From    SkyService `json:"from"`
	To      SkyService `json:"to"`
	Latency string     `json:"latency,omitempty"`
}

type DeployMap struct {
	Component []SkyService    `json:"components,omitempty"`
	Edges     []DeployMapEdge `json:"edges,omitempty"`
}

type SkyService struct {
	ComponentRef corev1.ObjectReference `json:"componentRef"`
	Manifest     string                 `json:"manifest,omitempty"`
	ProviderRef  ProviderRefSpec        `json:"providerRef,omitempty"`
	Conditions   []metav1.Condition     `json:"conditions,omitempty"`
}

type SkyComponent struct {
	Components corev1.ObjectReference `json:"component"`
	Provider   ProviderRefSpec        `json:"provider,omitempty"`
	// LocationConstraints specifies the location constraints for the SkyComponent
	// It declartively specifies the provider and region where the SkyComponent should be deployed
	LocationConstraint LocationConstraint `json:"locationConstraint,omitempty"`
	// VirtualServices specifies the virtual services that are required by the SkyComponent
	VirtualServices []VirtualService `json:"virtualServices,omitempty"`
}

// SkyObject represents a "Object" managed by SkyCluster
// This is a claim that maps to a cluster-scope object.kubernetes resource
type SkyObject struct {
	Name      string            `json:"name,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Manifest  runtime.RawExtension    `json:"manifest,omitempty"`
	ProviderRef ProviderRefSpec      `json:"providerRefSpec,omitempty"`
}

func (s *SkyObject) ManifestAsMap() (map[string]any, error) {
	var m map[string]any
	if len(s.Manifest.Raw) == 0 {
			return nil, nil
	}
	err := json.Unmarshal(s.Manifest.Raw, &m)
	return m, err
}

type ProviderRefSpec struct {
	Name        string `json:"name,omitempty"`
	Platform    string `json:"platform,omitempty"`
	Type        string `json:"type,omitempty"`
	Region      string `json:"region,omitempty"`
	RegionAlias string `json:"regionAlias,omitempty"`
	Zone        string `json:"zone,omitempty"`
	ConfigName  string `json:"configName,omitempty"`
}

type ConnectionSecret struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

func GetRegionAlias(region string) string {
	aliases := map[string]string{
		"scinet":         "scinet",
		"vaughan":        "vaughan",
		"us-east-1":      "us-east",
		"us-east-2":      "us-east",
		"us-west-1":      "us-west",
		"us-west-2":      "us-west",
		"eu-west-1":      "eu-west",
		"eu-west-2":      "eu-west",
		"eu-central-1":   "eu-central",
		"eu-central":     "eu-central",
		"eu-east-1":      "eu-east",
		"ap-south-1":     "ap-south",
		"ap-northeast-1": "ap-northeast",
		"ap-southeast-1": "ap-southeast",
		"sa-east-1":      "sa-east",
		"ca-central-1":   "ca-central",
		"me-south-1":     "me-south",
		"af-south-1":     "af-south",
	}

	normalized := strings.ToLower(strings.TrimSpace(region))

	if alias, found := aliases[normalized]; found {
		return alias
	}

	for key, alias := range aliases {
		if strings.Contains(normalized, key) || strings.Contains(key, normalized) {
			return alias
		}
	}

	return "unknown"
}


var (
	ReasonServiceNotReady = "ServiceNotReady"
)

var (
	ReasonsForServices = []string{"Unreachable", "ExecuteError", "ExecuteUnknown"}
)

type SkyScheduleSpec struct {
	// Interval is the time interval in seconds to wait before the next check
	Interval int `json:"interval,omitempty"`
	// Retries is the number of retries to be made before taking the failure action
	Retries int `json:"retries,omitempty"`
}

type MonitoringSpec struct {
	// Protocol is the protocol used for monitoring
	// +kubebuilder:validation:Enum=HTTP;HTTPS;TCP;SSH;http;https;tcp;ssh
	Protocol string `json:"protocol,omitempty"`
	// Host is the host endooint to connect and get the service status
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
	// CheckCommand is the command to be executed to check the status of the service
	// Only applicable for SSH protocol
	CheckCommand string `json:"checkCommand,omitempty"`
	// FailureAction is the action to take when the monitoring fails
	// +kubebuilder:validation:Enum=RECREATE;IGNORE;recreate;ignore
	FailureAction string `json:"failureAction,omitempty"`
	// ConnectionSecret is the secret that contains the credentials to access
	// the monitoring endpoint
	ConnectionSecret ConnectionSecret `json:"connectionSecret,omitempty"`
	// Schedule is the schedule information for the monitoring
	Schedule SkyScheduleSpec `json:"schedule,omitempty"`
}

type ManagedK8s struct {
	Name    string                     `json:"name,omitempty" yaml:"name"`
	NameLabel    string                `json:"nameLabel" yaml:"nameLabel"`
	Price   string                     `json:"price,omitempty" yaml:"price"`
	Overhead ManagedK8sOverhead      `json:"overhead,omitempty" yaml:"overhead"`
}

type ManagedK8sOverhead struct {
	Cost string 					 `json:"cost,omitempty" yaml:"cost"`
	Count int 						 `json:"count,omitempty" yaml:"count"`
	InstanceType string 			 `json:"instanceType,omitempty" yaml:"instanceType"`
}


type GPU struct {
	Enabled      bool   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty" yaml:"manufacturer,omitempty"`
	Count        int    `json:"count,omitempty" yaml:"count,omitempty"`
	Model        string `json:"model,omitempty" yaml:"model,omitempty"`
	Unit         string `json:"unit,omitempty" yaml:"unit,omitempty"`
	Memory       string `json:"memory,omitempty" yaml:"memory,omitempty"`
}

// used in svc group for abstract flavor spec (user-facing)
type FlavorSpec struct {
	VCPU string `json:"vcpu,omitempty" yaml:"vcpu,omitempty"`
	RAM  string `json:"ram,omitempty" yaml:"ram,omitempty"`
	GPU GPU `json:"gpu,omitempty" yaml:"gpu,omitempty"`
}