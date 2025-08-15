// +k8s:deepcopy-gen=package
package dep


type Dependency struct {
	NameRef   string `json:"name"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
}

type DependencyManager struct {
	// Dependencies is the field that will be "promoted" to the embedding struct.
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

// HasDependency checks if a dependency with a given kind and name exists.
// The receiver is a pointer to modify the Dependencies slice.
func (dm *DependencyManager) HasDependency(name, kind, ns string) bool {
	for _, dep := range dm.Dependencies {
		if dep.Kind == kind && dep.NameRef == name && dep.Namespace == ns {
			return true
		}
	}
	return false
}

// SetDependency adds or updates a dependency.
// This is a common pattern for "upserting" a dependency.
func (dm *DependencyManager) SetDependency(name, kind, ns string) {

	dep := Dependency{
		NameRef:   name,
		Kind:      kind,
		Namespace: ns,
	}

	// First, try to update an existing dependency.
	for i, existingDep := range dm.Dependencies {
		if existingDep.Kind == dep.Kind && existingDep.NameRef == dep.NameRef {
			// Update the existing dependency in place and return.
			dm.Dependencies[i] = dep
			return
		}
	}
	// If it doesn't exist, append the new dependency.
	dm.Dependencies = append(dm.Dependencies, dep)
}

// RemoveDependency removes a dependency with the given kind and name.
// This function efficiently rebuilds the slice without the removed item.
func (dm *DependencyManager) RemoveDependency(name, kind, ns string) {
	var newDependencies []Dependency
	for _, dep := range dm.Dependencies {
		if dep.Kind == kind && dep.NameRef == name && dep.Namespace == ns {
			// Skip this dependency and do nothing.
			continue
		}
		newDependencies = append(newDependencies, dep)
	}
	dm.Dependencies = newDependencies
}