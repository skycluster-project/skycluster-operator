package helper

func TruncatedName(name, newName string) string {
	max := 48 - len(newName) - 6 // "-" + ~5-char random suffix
	if len(name) > max {
		return name[:max] + newName
	}
	return name + newName
}