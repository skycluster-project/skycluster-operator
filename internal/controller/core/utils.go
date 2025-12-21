package core

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func SetTimeAnnotation(obj metav1.Object, key string, t metav1.Time) {
	anns := obj.GetAnnotations()
	if anns == nil {
		anns = map[string]string{}
	}
	anns[key] = t.UTC().Format(time.RFC3339) // serialize
	obj.SetAnnotations(anns)
}

// retrieve metav1.Time from annotations
func GetTimeAnnotation(obj metav1.Object, key string) (*metav1.Time, error) {
	anns := obj.GetAnnotations()
	v, ok := anns[key]
	if !ok {
		return nil, fmt.Errorf("annotation %q not found", key)
	}
	parsed, err := time.Parse(time.RFC3339, v) // parse
	if err != nil {
		return nil, err
	}
	mt := metav1.NewTime(parsed)
	return &mt, nil
}

// GetNestedField returns the nested field of a map[string]interface{} object
// It returns the nested field if it exists, and an error if it doesn't
// The fields are the keys to access the nested field
func GetNestedField(obj map[string]any, fields ...string) (map[string]any, error) {
	if len(fields) == 0 {
		return nil, errors.New("no fields provided")
	}
	m := obj
	for _, field := range fields {
		if val, ok := m[field].(map[string]interface{}); ok {
			m = val
		} else {
			return nil, fmt.Errorf("field %s not found in the object or its type is not map[string]interface{}", field)
		}
	}
	return m, nil // the last field is not found in the object
}

// getUniqueFlavors returns a list of unique flavors from all configmaps
func getUniqueFlavors(allConfigMap *corev1.ConfigMapList) []string {
	allFlavors := make([]string, 0)
	allFlavors_set := make(map[string]struct{}, 0)
	for _, cm := range allConfigMap.Items {
		for k := range cm.Data {
			if !strings.Contains(k, "skyvm_flavor") {
				continue
			}
			flavorName := strings.Split(k, "_")[2]
			if _, ok := allFlavors_set[flavorName]; ok {
				continue
			}
			allFlavors = append(allFlavors, flavorName)
			allFlavors_set[flavorName] = struct{}{}
		}
	}
	return allFlavors
}

// getCompatibleFlavors returns the flavors names that satisfy the minimum
// requirements for a compute resource. The flavors are in the format of "vCPU-RAM"
func getCompatibleFlavors(minCPU, minRAM float64, allFlavors []string) ([]string, error) {
	okFlavors := make([]string, 0)
	for _, skyFlavor := range allFlavors {
		cpu := strings.Split(skyFlavor, "-")[0]
		cpu = strings.Replace(cpu, "vCPU", "", -1)
		cpu_int, err1 := (strconv.Atoi(cpu))
		cpu_float := float64(cpu_int)
		ram := strings.Split(skyFlavor, "-")[1]
		ram = strings.Replace(ram, "GB", "", -1)
		ram_int, err2 := (strconv.Atoi(ram))
		ram_float := float64(ram_int)
		if err1 != nil || err2 != nil {
			if err1 != nil {
				return nil, errors.Wrap(err1, "Error converting flavor spec to int.")
			}
			if err2 != nil {
				return nil, errors.Wrap(err1, "Error converting flavor spec to int.")
			}
			// if there are error processing the flavors we ignore them and not add them to the list
			continue
		}
		if cpu_float >= minCPU && ram_float >= minRAM {
			okFlavors = append(okFlavors, fmt.Sprintf("%s|1", skyFlavor))
		}
	}
	return okFlavors, nil
}

// 4vCPU-8GB
// 4vCPU-4GB-3xLABEL
// use * for any number of chars
// use ? for a single char
func isValidComputeProfile(s string) bool {
	re := regexp.MustCompile(`^(\d+|\*)vCPU-(\d+|\*)GB(\*|-(\d+|\*)x.+)?$`)
	return re.MatchString(s)
}

func wildcardComputeProfileMatch(pattern, str string) bool {
	// Escape regex special chars
	regex := regexp.QuoteMeta(pattern)
	// Replace wildcards with regex equivalents
	regex = strings.ReplaceAll(regex, `\*`, ".*")
	regex = strings.ReplaceAll(regex, `\?`, ".")
	// Add anchors
	regex = "^" + regex + "$"
	re := regexp.MustCompile(regex)
	return re.MatchString(str)
}

////////////////////////////////////////////////////////////
// POD LOGGING UTILS

// Define what we actually need from the K8s API
type PodLogProvider interface {
	GetLogs(name string, opts *corev1.PodLogOptions) *rest.Request
}

// PodLogger abstracts the K8s log streaming call
type PodLogger interface {
	StreamLogs(ctx context.Context, namespace, name string, opts *corev1.PodLogOptions) (io.ReadCloser, error)
}

// Real implementation for production
type K8sPodLogger struct {
	KubeClient kubernetes.Interface
}

func (k *K8sPodLogger) StreamLogs(ctx context.Context, namespace, name string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	return k.KubeClient.CoreV1().Pods(namespace).GetLogs(name, opts).Stream(ctx)
}

// FakePodLogger is a mock implementation of the PodLogger interface
type FakePodLogger struct {
	LogOutput string
}

func (f *FakePodLogger) StreamLogs(ctx context.Context, ns, name string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.LogOutput)), nil
}

////////////////////////////////////////////////////////////
