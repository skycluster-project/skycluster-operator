package helper

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/samber/lo"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	zapCtrl "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/yaml"
)

const (
	SKYCLUSTER_NAMESPACE = "skycluster-system"
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
