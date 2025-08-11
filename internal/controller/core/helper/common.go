package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/samber/lo"
	"go.uber.org/zap/zapcore"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	zapCtrl "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/yaml"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

const (
	SKYCLUSTER_NAMESPACE = "skycluster-system"
	SKYCLUSTER_PV_NAME   = "skycluster-pv"
	UpdateThreshold      = time.Duration(cv1a1.DefaultRefreshHourImages) * time.Hour
	RequeuePollThreshold = 10 * time.Second
)

func customLoggerFormat() zapCtrl.EncoderConfigOption {
	return func(encoderConfig *zapcore.EncoderConfig) {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02T15:04:05Z")
		encoderConfig.EncodeName = func(name string, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(name)
		}
	}
}

func CustomLogger(logDir ...string) zapCtrl.Opts {
	var file *os.File
	var err error
	if len(logDir) > 0 && logDir[0] != "" {
		file, err = os.Create(filepath.Join(logDir[0], "manager.log"))
		if err != nil {
			panic(err)
		}
	}
	if file == nil {
		file = os.Stdout // Fallback to stdout if no logDir is provided
	}
	opts := zapCtrl.Options{
		Development: true,
		EncoderConfigOptions: []zapCtrl.EncoderConfigOption{
			customLoggerFormat(),
		},
		DestWriter: file,
	}
	return zapCtrl.UseFlagOptions(&opts)
}

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

func ContainerTerminatedReason(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil &&
			cs.State.Terminated.Reason != "" {
			return cs.State.Terminated.Reason
		}
	}
	return ""
}

func ContainerTerminatedMessage(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil &&
			cs.State.Terminated.Reason == "Completed" &&
			cs.State.Terminated.Message != "" {
			return cs.State.Terminated.Message
		}
	}
	return ""
}

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

func ProviderProfileCMUpdate(ctx context.Context, c client.Client, pf *cv1a1.ProviderProfile, yamlDataStr, key string) error {

	// defaultZone, ok := lo.Find(pf.Spec.Zones, func(zone cv1a1.ZoneSpec) bool {
	// 	return zone.DefaultZone
	// })
	ll := DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")

	cmList := &corev1.ConfigMapList{}
	if err := c.List(ctx, cmList, client.MatchingLabels(ll), client.InNamespace(SKYCLUSTER_NAMESPACE)); err != nil {
		return fmt.Errorf("unable to list ConfigMaps for images: %w", err)
	}
	if len(cmList.Items) != 1 {
		return fmt.Errorf("error listing ConfigMaps for images: expected 1, got %d", len(cmList.Items))
	}
	cm := cmList.Items[0]

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[key] = yamlDataStr

	if err := c.Update(ctx, &cm); err != nil {
		return fmt.Errorf("failed to update ConfigMap for images: %w", err)
	}

	return nil
}
