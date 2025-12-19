package utils

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func ApplyYAML(ctx context.Context, c ctrlclient.Client, scheme *runtime.Scheme, path string) error {

	files, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fullPath := filepath.Join(path, file.Name())
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return err
		}

		decoder := yaml.NewYAMLOrJSONDecoder(
			os.NewFile(0, "/dev/null"),
			4096,
		)
		decoder = yaml.NewYAMLOrJSONDecoder(
			bytes.NewReader(data),
			4096,
		)

		for {
			obj := &unstructured.Unstructured{}
			if err := decoder.Decode(obj); err != nil {
				break
			}

			obj.SetManagedFields(nil)
			if err := c.Create(ctx, obj); err != nil {
				// ignore AlreadyExists
				if !apierrors.IsAlreadyExists(err) {
					return err
				}
			}
		}
	}

	return nil
}
