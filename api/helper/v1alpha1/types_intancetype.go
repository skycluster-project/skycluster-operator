package v1alpha1

type Spot struct {
	Price   string `json:"price,omitempty" yaml:"price,omitempty"`
	Enabled bool   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

type GPU struct {
	Enabled      bool   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty" yaml:"manufacturer,omitempty"`
	Count        int    `json:"count,omitempty" yaml:"count,omitempty"`
	Model        string `json:"model,omitempty" yaml:"model,omitempty"`
	Unit         string `json:"unit,omitempty" yaml:"unit,omitempty"`
	Memory       string `json:"memory,omitempty" yaml:"memory,omitempty"`
}

type InstanceOffering struct {
	Name        string   `json:"name,omitempty" yaml:"name,omitempty"`
	NameLabel   string   `json:"nameLabel,omitempty" yaml:"nameLabel,omitempty"`
	VCPUs       int      `json:"vcpus,omitempty" yaml:"vcpus,omitempty"`
	RAM         string   `json:"ram,omitempty" yaml:"ram,omitempty"`
	Price       string   `json:"price,omitempty" yaml:"price,omitempty"`
	GPU         GPU      `json:"gpu,omitempty" yaml:"gpu,omitempty"`
	Generation  string   `json:"generation,omitempty" yaml:"generation,omitempty"`
	VolumeTypes []string `json:"volumeTypes,omitempty" yaml:"volumeTypes,omitempty"`
	Spot        Spot     `json:"spot,omitempty" yaml:"spot,omitempty"`
}

// used in svc group for abstract flavor spec (user-facing)
type ComputeFlavor struct {
	VCPUs string `json:"vcpus,omitempty" yaml:"vcpus,omitempty"`
	RAM   string `json:"ram,omitempty" yaml:"ram,omitempty"`
	GPU   GPU    `json:"gpu,omitempty" yaml:"gpu,omitempty"`
}
