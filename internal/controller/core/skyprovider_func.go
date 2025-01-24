package core

import (
	"context"
	"strconv"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func updateIPCidrConfigMap(kubeClient client.Client, configMap *corev1.ConfigMap, currentIpSubnet string) error {
	data := configMap.Data
	currentIpSubnetInt, err := strconv.Atoi(currentIpSubnet)
	if err != nil {
		return errors.Wrap(err, "failed to convert currentIpSubnet to int")
	}
	currentIpSubnetInt++
	data["currentIpSubnet"] = strconv.Itoa(currentIpSubnetInt)
	if err := kubeClient.Update(context.Background(), configMap); err != nil {
		return errors.Wrap(err, "failed to update ConfigMap")
	}
	return nil
}

func getIpCidrPartsFromSkyProvider(kubeClient client.Client, obj *corev1alpha1.SkyProvider) (string, string, *corev1.ConfigMap, error) {
	providerName := obj.Spec.ProviderRef.ProviderName

	// get a config map with label config-type: ip-cidr-ranges
	configMaps := &corev1.ConfigMapList{}
	listOptions := &client.MatchingLabels{
		"skycluster.io/config-type":   "ip-cidr-ranges",
		"skycluster.io/provider-name": providerName,
	}
	if err := kubeClient.List(context.Background(), configMaps, listOptions); err != nil {
		err := errors.Wrap(err, "failed to list ConfigMaps for ip-cidr-ranges")
		return "", "", nil, err
	}

	if len(configMaps.Items) == 0 {
		err := errors.New("No ConfigMap found with label config-type: ip-cidr-ranges")
		return "", "", nil, err
	}
	// There should be only one config map matching the labels
	configMap := &configMaps.Items[0]
	// get the data and based on the values of the fields returns their values
	data := configMap.Data
	// check if any fields is equal to 'providerName'
	ipGroup, ok1 := data["ipGroup"]
	currentIpSubnet, ok2 := data["currentIpSubnet"]

	if !ok1 || !ok2 {
		err := errors.New("No IP CIDR range found for the provider")
		return "", "", nil, err
	}
	return ipGroup, currentIpSubnet, configMap, nil
}
