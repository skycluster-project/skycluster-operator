package v1alpha1

import (
	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
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
	ConnectionSecret corev1alpha1.ConnectionSecret `json:"connectionSecret,omitempty"`
	// Schedule is the schedule information for the monitoring
	Schedule SkyScheduleSpec `json:"schedule,omitempty"`
}
