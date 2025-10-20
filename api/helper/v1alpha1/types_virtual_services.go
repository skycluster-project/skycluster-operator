package v1alpha1


type ManagedK8s struct {
	Name    string                     `json:"name,omitempty" yaml:"name"`
	NameLabel    string                `json:"nameLabel" yaml:"nameLabel"`
	Price   string                     `json:"price,omitempty" yaml:"price"`
	Overhead ManagedK8sOverhead      `json:"overhead,omitempty" yaml:"overhead"`
}

type ManagedK8sOverhead struct {
	Cost string 					 `json:"cost,omitempty" yaml:"cost"`
	Count int 						 `json:"count,omitempty" yaml:"count"`
	InstanceType string 			 `json:"instanceType,omitempty" yaml:"instanceType"`
}

// ComputeProfile is defined as InstanceOffering in InstanceType
// type ComputeProfile struct {
// }