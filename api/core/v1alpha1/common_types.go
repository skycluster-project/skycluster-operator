package v1alpha1

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	SKYCLUSTER_NAMESPACE           = "skycluster"
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
	Required  LocationRequiredRuleSet  `json:"required,omitempty"`
	Permitted LocationPermittedRuleSet `json:"permitted,omitempty"`
}

type LocationPermittedRuleSet struct {
	AllOf []ProviderRefSpec `json:"allOf,omitempty"`
}

type LocationRequiredRuleSet struct {
	AllOf []LocationRule `json:"allOf,omitempty"`
}

type LocationRule struct {
	AnyOf       []ProviderRefSpec `json:"anyOf,omitempty"`
	ProviderRef *ProviderRefSpec  `json:"providerRef,omitempty"`
}

type VirtualService struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
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

type ProviderRefSpec struct {
	ProviderName        string `json:"providerName,omitempty"`
	ProviderRegion      string `json:"providerRegion,omitempty"`
	ProviderRegionAlias string `json:"providerRegionAlias,omitempty"`
	ProviderType        string `json:"providerType,omitempty"`
	ProviderZone        string `json:"providerZone,omitempty"`
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
