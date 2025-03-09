package core

import (
	"context"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// func updateIPCidrConfigMap(kubeClient client.Client, configMap *corev1.ConfigMap, currentIpSubnet int) error {
// 	if currentIpSubnet >= 254 {
// 		currentIpSubnet = 10
// 	} else if currentIpSubnet < 10 {
// 		currentIpSubnet = 10
// 	}
// 	data := configMap.Data
// 	data["currentIpSubnet"] = strconv.Itoa(currentIpSubnet)
// 	if err := kubeClient.Update(context.Background(), configMap); err != nil {
// 		return errors.Wrap(err, "failed to update ConfigMap")
// 	}
// 	return nil
// }

func getSubnetCidr(kubeClient client.Client, obj corev1alpha1.SkyProvider) (string, string, *corev1.ConfigMap, error) {
	providerName := obj.Spec.ProviderRef.ProviderName

	// get a config map with label config-type: subnet-cidr
	configMaps := &corev1.ConfigMapList{}
	listOptions := &client.MatchingLabels{
		"skycluster.io/config-type":     corev1alpha1.SKYCLUSTER_ProvdiderMappings_LABEL,
		"skycluster.io/provider-name":   providerName,
		"skycluster.io/provider-region": obj.Spec.ProviderRef.ProviderRegion,
		// provider-type equal to default only filter regional settings
		// which should have the subnet CIDR information
		"skycluster.io/provider-type": "default",
	}
	if err := kubeClient.List(context.Background(), configMaps, listOptions); err != nil {
		err := errors.Wrap(err, "failed to list ConfigMaps for subnet-cidr")
		return "", "", nil, err
	}

	if len(configMaps.Items) == 0 {
		err := errors.New("No ConfigMap found with label config-type: subnet-cidr")
		return "", "", nil, err
	}
	// There should be only one config map matching the labels
	configMap := &configMaps.Items[0]
	// get the data and based on the values of the fields returns their values
	data := configMap.Data
	// check if any fields is equal to 'providerName'
	vpcCidr, ok1 := data[corev1alpha1.SKYCLUSTER_VPCCidrField_LABEL]
	subnetIndex, ok2 := data[corev1alpha1.SKYCLUSTER_SubnetIndexField_LABEL]
	if !ok1 {
		err := errors.New("No IP CIDR range found for the provider")
		return "", "", nil, err
	}
	if !ok2 {
		err := errors.New("No subnet index found for the provider")
		return "", "", nil, err
	}
	return vpcCidr, subnetIndex, configMap, nil
}

func ListSkyProviderByLabels(
	kubeClient client.Client,
	searchLabels map[string]string,
	refType map[string]string) (*corev1alpha1.SkyProviderList, error) {
	// Iterate over the list of objects with given group, version and kind
	// and search for the object with the given labels
	skyProviderList := &corev1alpha1.SkyProviderList{}
	skyProviderList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   refType["group"],
		Version: refType["version"],
		Kind:    refType["kind"],
	})
	if err := kubeClient.List(context.Background(), skyProviderList, client.MatchingLabels(searchLabels)); err != nil {
		return nil, err
	}
	return skyProviderList, nil
}
