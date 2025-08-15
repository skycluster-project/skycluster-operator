package v1alpha1

type Condition string

const (
	Ready    Condition = "Ready"
	Healthy  Condition = "Healthy"
	Degraded Condition = "Degraded"

	// For Runner Pods
	JobRunning   Condition = "JobRunning"
	JobSucceeded Condition = "JobSucceeded"

	// Others
	ResyncRequired Condition = "ResyncRequired"
)
