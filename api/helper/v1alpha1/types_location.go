package v1alpha1

type LocationConstraint struct {
	// Required specifies location sets that must be satisfied (AND logic).
	Required []LocationCondition `json:"required,omitempty"`
	// Permitted specifies locations that are allowed (OR logic).
	Permitted []ProviderRefSpec `json:"permitted,omitempty"`
}

// LocationCondition represents a group of alternative location rules (OR logic).
type LocationCondition struct {
	AnyOf []ProviderRefSpec `json:"anyOf,omitempty"`
}
