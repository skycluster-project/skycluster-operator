package core

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	depv1a1 "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/dep"
)

func SetupImagePod(name, nameLabel, namespace string, provider *cv1a1.ProviderProfile) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/job-type":          nameLabel,
				"skycluster.io/provider-profile":  provider.Name,
				"skycluster.io/managed-by":        "skycluster",
				"skycluster.io/provider-platform": provider.Spec.Platform,
				"skycluster.io/provider-region":   provider.Spec.Region,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "harvest",
					Image: "busybox",
				},
				{
					Name:  "runner",
					Image: "etesami/image-finder:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					LastProbeTime:      metav1.NewTime(time.Now()),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: 1,
					Status:             "True",
					Type:               corev1.PodInitialized,
				},
				{
					LastProbeTime:      metav1.NewTime(time.Now()),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: 1,
					Status:             "True",
					Type:               corev1.PodReady,
				},
				{
					LastProbeTime:      metav1.NewTime(time.Now()),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: 1,
					Status:             "True",
					Type:               corev1.PodScheduled,
				},
			},
			Phase: corev1.PodSucceeded,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "harvest",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason: "Completed",
						},
					},
				},
			},
		},
	}
}

func SetupImageResource(ctx context.Context, resourceName, namespace string) *cv1a1.Image {
	return &cv1a1.Image{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
		},
		Spec: cv1a1.ImageSpec{
			ProviderRef: resourceName,
			Images: []cv1a1.ImageOffering{
				{
					NameLabel: "ubuntu-20.04",
					Pattern:   "*hvm-ssd*/ubuntu-focal-20.04-amd64-server*",
				},
				{
					NameLabel: "ubuntu-22.04",
					Pattern:   "*hvm-ssd*/ubuntu-jammy-22.04-amd64-server*",
				},
			},
		},
	}
}

func SetupProviderProfileConfigMap(resourceName, namespace string, provider *cv1a1.ProviderProfile) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/managed-by":        "skycluster",
				"skycluster.io/provider-profile":  provider.Name,
				"skycluster.io/provider-platform": provider.Spec.Platform,
				"skycluster.io/provider-region":   provider.Spec.Region,
			},
		},
		Data: configMapData(),
	}
}

func configMapData() map[string]string {
	return map[string]string{
		"flavors.yaml": `- zone: us-east-1a
zoneOfferings:
- name: g5.12xlarge
nameLabel: 48vCPU-192GB-4xA10G-22GB
vcpus: 48
ram: 192GB
price: "5.67"
gpu:
enabled: true
manufacturer: NVIDIA
count: 4
model: A10G
memory: 22GB
spot:
price: "2.12"
enabled: true
- name: g5.16xlarge
nameLabel: 64vCPU-256GB-1xA10G-22GB
vcpus: 64
ram: 256GB
price: "4.10"
gpu:
enabled: true
manufacturer: NVIDIA
count: 1
model: A10G
memory: 22GB
spot:
price: "1.07"
enabled: true`,
		"images.yaml": `- nameLabel: ubuntu-20.04
name: ami-0fb0b230890ccd1e6
zone: us-east-1a
- nameLabel: ubuntu-22.04
name: ami-0e70225fadb23da91
zone: us-east-1a
- nameLabel: ubuntu-24.04
name: ami-07033cb190109bd1d
zone: us-east-1a
- nameLabel: ubuntu-20.04
name: ami-0fb0b230890ccd1e6
zone: us-east-1b
- nameLabel: ubuntu-22.04
name: ami-0e70225fadb23da91
zone: us-east-1b
- nameLabel: ubuntu-24.04
name: ami-07033cb190109bd1d
zone: us-east-1b`,
		"managed-k8s.yaml": `- name: EKS
nameLabel: ManagedKubernetes
overhead:
	cost: "0.096"
	count: 1
	instanceType: m5.xlarge
price: "0.10"`,
	}
}

func SetupProviderCredSecret(resourceName, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/managed-by":        "skycluster",
				"skycluster.io/provider-platform": "aws",
				"skycluster.io/secret-role":       "credentials",
			},
		},
		Data: map[string][]byte{
			"aws_access_key_id":     []byte("xyzswerd"),
			"aws_secret_access_key": []byte("shdrugydh"),
		},
		Type: "Opaque",
	}
}

func SetProviderProfileStatus(provider *cv1a1.ProviderProfile, resourceName, namespace string) {
	provider.Status = cv1a1.ProviderProfileStatus{
		Platform: "aws",
		Region:   "us-east-1",
		Zones: []cv1a1.ZoneSpec{
			{Name: "us-east-1a", Enabled: true, DefaultZone: true, Type: "cloud"},
		},
		DependencyManager: depv1a1.DependencyManager{
			Dependencies: []depv1a1.Dependency{
				{
					NameRef:   "aws-us-east-1",
					Kind:      "ConfigMap",
					Namespace: namespace,
				},
				{
					NameRef:   resourceName,
					Kind:      "Image",
					Namespace: namespace,
				},
			},
		},
		EgressCostSpecs: []cv1a1.EgressCostSpec{
			{
				Type: "internet",
				Unit: "GB",
				Tiers: []cv1a1.EgressTier{
					{FromGB: 0, ToGB: 10000, PricePerGB: "0.09"},
				},
			},
			{
				Type: "inter-zone",
				Unit: "GB",
				Tiers: []cv1a1.EgressTier{
					{FromGB: 0, ToGB: 10000, PricePerGB: "0.01"},
				},
			},
			{
				Type: "inter-region",
				Unit: "GB",
				Tiers: []cv1a1.EgressTier{
					{FromGB: 0, ToGB: 10000, PricePerGB: "0.02"},
				},
			},
		},
		ObservedGeneration: 1,
	}
}
