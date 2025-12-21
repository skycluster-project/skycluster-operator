package core

import (
	"context"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

////////////////////////////////////////////////////////////
// POD LOGGING UTILS

// Define what we actually need from the K8s API
// type PodLogProvider interface {
// 	GetLogs(name string, opts *corev1.PodLogOptions) *rest.Request
// }

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
