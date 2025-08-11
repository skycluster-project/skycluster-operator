package v1alpha1

const (
	CondReady       = "Ready"
	CondReasonDone  = "Finished"
	CondReasonRun   = "Running"
	CondReasonStale = "SpecChangedWhileRunning"
)
