package helper

import (
	"strings"
)

func TruncatedName(name, newName string) string {
	max := 48 - len(newName) - 6 // "-" + ~5-char random suffix
	if len(name) > max {
		return name[:max] + newName
	}
	return name + newName
}



// EnsureK8sName returns a version of name that conforms to Kubernetes resource
// name rules (DNS-1123 labels separated by dots). It removes invalid characters,
// lowercases, enforces per-label max length 63 and total max length 253,
// and ensures each label starts/ends with an alphanumeric (replacing empty labels with "a").
func EnsureK8sName(name string) string {
	if name == "" {
		return "a"
	}

	cleanLabel := func(s string) string {
		s = strings.ToLower(s)
		var b strings.Builder
		b.Grow(len(s))
		for i := 0; i < len(s); i++ {
			ch := s[i]
			if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
				b.WriteByte(ch)
			}
		}
		out := b.String()
		out = strings.TrimLeft(out, "-")
		out = strings.TrimRight(out, "-")
		if out == "" {
			out = "a"
		}
		if len(out) > 63 {
			out = out[:63]
			out = strings.TrimRight(out, "-")
			if out == "" {
				out = "a"
			}
		}
		return out
	}

	parts := strings.Split(name, ".")
	for i, p := range parts {
		if p == "" {
			parts[i] = "a"
		} else {
			parts[i] = cleanLabel(p)
		}
	}

	joined := strings.Join(parts, ".")
	if len(joined) <= 253 {
		return joined
	}

	// Truncate last label until total length <= 253, keeping rules.
	for len(joined) > 253 {
		last := parts[len(parts)-1]
		over := len(joined) - 253
		newLen := len(last) - over
		if newLen < 1 {
			newLen = 1
		}
		if newLen > len(last) {
			newLen = len(last)
		}
		last = last[:newLen]
		last = strings.TrimRight(last, "-")
		if last == "" {
			last = "a"
		}
		parts[len(parts)-1] = last
		joined = strings.Join(parts, ".")
	}

	return joined
}