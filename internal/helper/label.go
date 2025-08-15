package helper

import (
	"github.com/samber/lo"
)

func DefaultLabels(p, r, z string) map[string]string {
	l := map[string]string{
		"skycluster.io/managed-by":  "skycluster",
	}
	l = lo.Assign(l, lo.Ternary(p != "",
		map[string]string{"skycluster.io/provider-platform": p}, nil))
	l = lo.Assign(l, lo.Ternary(r != "",
		map[string]string{"skycluster.io/provider-region": r}, nil))
	l = lo.Assign(l, lo.Ternary(z != "",
		map[string]string{"skycluster.io/provider-zone": z}, nil))
	return l
}
