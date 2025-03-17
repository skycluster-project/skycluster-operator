package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	SKYCLUSTER_NAMESPACE            = "skycluster"
	SKYCLUSTER_API                  = "skycluster.io"
	SKYCLUSTER_COREGROUP            = "core." + SKYCLUSTER_API
	SKYCLUSTER_XRDsGROUP            = "xrds." + SKYCLUSTER_API
	SKYCLUSTER_VERSION              = "v1alpha1"
	SKYCLUSTER_MANAGEDBY_LABEL      = SKYCLUSTER_API + "/managed-by"
	SKYCLUSTER_MANAGEDBY_VALUE      = "skycluster"
	SKYCLUSTER_PAUSE_LABEL          = SKYCLUSTER_API + "/pause"
	SKYCLUSTER_ORIGINAL_NAME_LABEL  = SKYCLUSTER_API + "/original-name"
	SKYCLUSTER_CONFIGTYPE_LABEL     = SKYCLUSTER_API + "/config-type"
	SKYCLUSTER_PROVIDERNAME_LABEL   = SKYCLUSTER_API + "/provider-name"
	SKYCLUSTER_PROVIDERREGION_LABEL = SKYCLUSTER_API + "/provider-region"
	SKYCLUSTER_PROVIDERZONE_LABEL   = SKYCLUSTER_API + "/provider-zone"
	SKYCLUSTER_PROVIDERTYPE_LABEL   = SKYCLUSTER_API + "/provider-type"
	SKYCLUSTER_PROVIDERID_LABEL     = SKYCLUSTER_API + "/provider-identifier"
	SKYCLUSTER_PROJECTID_LABEL      = SKYCLUSTER_API + "/project-id"

	// SkyClusterConfigType values
	SKYCLUSTER_VPCCidrField_LABEL      = "vpcCidr"
	SKYCLUSTER_SubnetIndexField_LABEL  = "subnetIndex"
	SKYCLUSTER_ProvdiderMappings_LABEL = "provider-mappings"
	SKYCLUSTER_VSERVICES_LABEL         = "provider-vservices"
)

type LocationConstraint struct {
	Required  []ProviderRefSpec `json:"required,omitempty"`
	Permitted []ProviderRefSpec `json:"permitted,omitempty"`
}

type VirtualService struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type DeployMapEdge struct {
	From    SkyComponent `json:"from"`
	To      SkyComponent `json:"to"`
	Latency string       `json:"latency,omitempty"`
}

type DeployMap struct {
	Component []SkyComponent  `json:"components,omitempty"`
	Edges     []DeployMapEdge `json:"edges,omitempty"`
}

type SkyService struct {
	Name        string             `json:"name,omitempty"`
	Kind        string             `json:"kind,omitempty"`
	APIVersion  string             `json:"apiVersion,omitempty"`
	Manifest    string             `json:"manifest,omitempty"`
	ProviderRef ProviderRefSpec    `json:"providerRef,omitempty"`
	Conditions  []metav1.Condition `json:"conditions,omitempty"`
}

type SkyComponent struct {
	Component corev1.ObjectReference `json:"component"`
	Provider  ProviderRefSpec        `json:"provider,omitempty"`
	// LocationConstraints specifies the location constraints for the SkyComponent
	// It declartively specifies the provider and region where the SkyComponent should be deployed
	LocationConstraint LocationConstraint `json:"locationConstraint,omitempty"`
	// VirtualServices specifies the virtual services that are required by the SkyComponent
	VirtualServices []VirtualService `json:"virtualServices,omitempty"`
}

type ProviderRefSpec struct {
	ProviderName        string `json:"providerName,omitempty"`
	ProviderRegion      string `json:"providerRegion,omitempty"`
	ProviderRegionAlias string `json:"providerRegionAlias,omitempty"`
	ProviderType        string `json:"providerType,omitempty"`
	ProviderZone        string `json:"providerZone,omitempty"`
}

type MonitoringMetricSpec struct {
	Type     string                 `json:"type"`
	Resource corev1.ObjectReference `json:"resource,omitempty"`
	Metric   MetricSpec             `json:"metric,omitempty"`
}

type MetricSpec struct {
	Endpoint string `json:"endpoint"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}
