package core

import (
	"errors"
	"fmt"
)

func GetNestedField(obj map[string]interface{}, fields ...string) (map[string]interface{}, error) {
	if len(fields) == 0 {
		return nil, errors.New("no fields provided")
	}
	m := obj
	for _, field := range fields {
		if val, ok := m[field].(map[string]interface{}); ok {
			m = val
		} else {
			return nil, errors.New(fmt.Sprintf("field %s not found in the object or its type is not map[string]interface{}", field))
		}
	}
	return m, nil // the last field is not found in the object
}
