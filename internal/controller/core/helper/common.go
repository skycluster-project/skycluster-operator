package helper

import (
	"os"
	"path/filepath"

	"go.uber.org/zap/zapcore"
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

func EncodeObjectToYAML(obj interface{}) (string, error) {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
