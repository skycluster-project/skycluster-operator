package v1alpha1

import (
	"encoding/json"
	"strings"

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

	ReasonServiceNotReady = "ServiceNotReady"
	ReasonsForServices = []string{"Unreachable", "ExecuteError", "ExecuteUnknown"}
)

type ComponentRef struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Spec       *runtime.RawExtension `json:"spec,omitempty"`
}

type ConnectionSecret struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

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

func (s *SkyService) ManifestAsMap() (map[string]any, error) {
	var m map[string]any
	if s.Manifest == "" {return nil, nil}
	err := json.Unmarshal([]byte(s.Manifest), &m)
	return m, err
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
	if alias, found := aliases[normalized]; found {return alias}

	for key, alias := range aliases {
		if strings.Contains(normalized, key) || strings.Contains(key, normalized) {
			return alias
		}
	}

	return "unknown"
}

// type ManagedK8s struct {
// 	Name    string                     `json:"name,omitempty" yaml:"name"`
// 	NameLabel    string                `json:"nameLabel" yaml:"nameLabel"`
// 	Price   string                     `json:"price,omitempty" yaml:"price"`
// 	Overhead ManagedK8sOverhead      `json:"overhead,omitempty" yaml:"overhead"`
// }

// type ManagedK8sOverhead struct {
// 	Cost string 					 `json:"cost,omitempty" yaml:"cost"`
// 	Count int 						 `json:"count,omitempty" yaml:"count"`
// 	InstanceType string 			 `json:"instanceType,omitempty" yaml:"instanceType"`
// }