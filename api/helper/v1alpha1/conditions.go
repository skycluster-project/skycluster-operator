package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionReady    string = "Ready"
	ConditionHealthy  string = "Healthy"
	ConditionDegraded string = "Degraded"

	// For Runner Pods
	ConditionFinish    string = "Finished"
	ConditionRunning   string = "Running"
	ConditionSucceeded string = "Succeeded"
	ConditionStale     string = "SpecChangedWhileRunning"
)

const (
	ConditionTrue    string = string(metav1.ConditionTrue)
	ConditionFalse   string = string(metav1.ConditionFalse)
	ConditionUnknown string = string(metav1.ConditionUnknown)
)

const (
	ReasonProviderProfileNotFound string = "ProviderProfileNotFound"
)
