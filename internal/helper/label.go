package helper

import (
	"github.com/samber/lo"
)

func DefaultLabels(p, r, z string) map[string]string {
	l := map[string]string{
		"skycluster.io/managed-by":  "skycluster",
		"skycluster.io/config-type": "provider-profile",
	}
	l = lo.Assign(l, lo.Ternary(p != "",
		map[string]string{"skycluster.io/provider-platform": p}, nil))
	l = lo.Assign(l, lo.Ternary(r != "",
		map[string]string{"skycluster.io/provider-region": r}, nil))
	l = lo.Assign(l, lo.Ternary(z != "",
		map[string]string{"skycluster.io/provider-zone": z}, nil))
	return l
}

func DefaultPodLabels(platform, region string) map[string]string {
	return map[string]string{
		"skycluster.io/managed-by":        "skycluster",
		"skycluster.io/provider-platform": platform,
		"skycluster.io/provider-region":   region,
	}
}