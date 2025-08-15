package v1alpha1

type Condition string

const (
	Ready    Condition = "Ready"
	Healthy  Condition = "Healthy"
	Degraded Condition = "Degraded"

	// For Runner Pods
	Finish    Condition = "Finished"
	Running   Condition = "Running"
	Succeeded Condition = "Succeeded"

	// Others
	NeedsFullReconcile Condition = "NeedsFullReconcile"
)
