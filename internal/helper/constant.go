package helper

import "time"

const (
	SKYCLUSTER_NAMESPACE = "skycluster-system"
	SKYCLUSTER_PV_NAME   = "skycluster-pv"

	NormalUpdateThreshold = 12 * time.Hour
	RequeuePollThreshold  = 10 * time.Second

	FN_Dependency = "skycluster.io/fn-dependency"
)
