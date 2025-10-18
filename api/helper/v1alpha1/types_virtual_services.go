package v1alpha1


type ManagedK8s struct {
	Name    string                     `json:"name,omitempty"`
	Price   string                     `json:"price,omitempty"`
	Overhead ManagedK8sOverhead      `json:"overhead,omitempty"`
}

type ManagedK8sOverhead struct {
	Cost string 					 `json:"cost,omitempty"`
	Count int 						 `json:"count,omitempty"`
	InstanceType string 			 `json:"instanceType,omitempty"`
}