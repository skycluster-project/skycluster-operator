package encoding

import (
	"encoding/json"

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
	data, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

