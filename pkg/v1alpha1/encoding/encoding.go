package encoding

import (
	"encoding/json"
	"errors"

	"gopkg.in/yaml.v3"
)

func EncodeJSONStringToYAML(jsonString string) (string, error) {
	var obj interface{}
	if err := json.Unmarshal([]byte(jsonString), &obj); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func EncodeObjectToYAML(obj interface{}) (string, error) {
	if obj == nil {
		return "", errors.New("object to encode cannot be nil")
	}
	data, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
