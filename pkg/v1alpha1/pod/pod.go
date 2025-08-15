package pod

import (
	corev1 "k8s.io/api/core/v1"
)

func ContainerTerminatedReason(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil &&
			cs.State.Terminated.Reason != "" {
			return cs.State.Terminated.Reason
		}
	}
	return ""
}

func ContainerTerminatedMessage(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil &&
			cs.State.Terminated.Reason == "Completed" &&
			cs.State.Terminated.Message != "" {
			return cs.State.Terminated.Message
		}
	}
	return ""
}