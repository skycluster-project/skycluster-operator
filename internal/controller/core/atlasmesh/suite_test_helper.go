package core

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

func configMapDataProviderE2E() map[string]string {
	return map[string]string{
		"aws": `
      - vpcCidr: 10.33.0.0/16
        subnets:
        - type: public
          cidr: 10.33.0.0/19
        - type: private
          cidr: 10.33.32.0/19
        serviceCidr: 10.255.0.0/16
        podCidr:
          cidr: 10.33.128.0/17
          public: 10.33.128.0/18
          private: 10.33.192.0/18
      - vpcCidr: 10.34.0.0/16
        subnets:
        - type: public
          cidr: 10.34.0.0/19
        - type: private
          cidr: 10.34.32.0/19
        serviceCidr: 10.254.0.0/16
        podCidr:
          cidr: 10.34.128.0/17
          public: 10.34.128.0/18
          private: 10.34.192.0/18
      - vpcCidr: 10.35.0.0/16
        subnets:
        - type: public
          cidr: 10.35.0.0/19
        - type: private
          cidr: 10.35.32.0/19
        serviceCidr: 10.253.0.0/16
        podCidr:
          cidr: 10.35.128.0/17
          public: 10.35.128.0/18
          private: 10.35.192.0/18
      - vpcCidr: 10.36.0.0/16
        subnets:
        - type: public
          cidr: 10.36.0.0/19
        - type: private
          cidr: 10.36.32.0/19
        serviceCidr: 10.252.0.0/16
        podCidr:
          cidr: 10.36.128.0/17
          public: 10.36.128.0/18
          private: 10.36.192.0/18
      - vpcCidr: 10.37.0.0/16
        subnets:
        - type: public
          cidr: 10.37.0.0/19
        - type: private
          cidr: 10.37.32.0/19
        serviceCidr: 10.251.0.0/16
        podCidr:
          cidr: 10.37.128.0/17
          public: 10.37.128.0/18
          private: 10.37.192.0/18
      - vpcCidr: 10.38.0.0/16
        subnets:
        - type: public
          cidr: 10.38.0.0/19
        - type: private
          cidr: 10.38.32.0/19
        serviceCidr: 10.250.0.0/16
        podCidr:
          cidr: 10.38.128.0/17
          public: 10.38.128.0/18
          private: 10.38.192.0/18
      - vpcCidr: 10.39.0.0/16
        subnets:
        - type: public
          cidr: 10.39.0.0/19
        - type: private
          cidr: 10.39.32.0/19
        serviceCidr: 10.249.0.0/16
        podCidr:
          cidr: 10.39.128.0/17
          public: 10.39.128.0/18
          private: 10.39.192.0/18
      - vpcCidr: 10.40.0.0/16
        subnets:
        - type: public
          cidr: 10.40.0.0/19
        - type: private
          cidr: 10.40.32.0/19
        serviceCidr: 10.248.0.0/16
        podCidr:
          cidr: 10.40.128.0/17
          public: 10.40.128.0/18
          private: 10.40.192.0/18
      - vpcCidr: 10.41.0.0/16
        subnets:
        - type: public
          cidr: 10.41.0.0/19
        - type: private
          cidr: 10.41.32.0/19
        serviceCidr: 10.247.0.0/16
        podCidr:
          cidr: 10.41.128.0/17
          public: 10.41.128.0/18
          private: 10.41.192.0/18
      - vpcCidr: 10.42.0.0/16
        subnets:
        - type: public
          cidr: 10.42.0.0/19
        - type: private
          cidr: 10.42.32.0/19
        serviceCidr: 10.246.0.0/16
        podCidr:
          cidr: 10.42.128.0/17
          public: 10.42.128.0/18
          private: 10.42.192.0/18
      - vpcCidr: 10.43.0.0/16
        subnets:
        - type: public
          cidr: 10.43.0.0/19
        - type: private
          cidr: 10.43.32.0/19
        serviceCidr: 10.245.0.0/16
        podCidr:
          cidr: 10.43.128.0/17
          public: 10.43.128.0/18
          private: 10.43.192.0/18
      - vpcCidr: 10.44.0.0/16
        subnets:
        - type: public
          cidr: 10.44.0.0/19
        - type: private
          cidr: 10.44.32.0/19
        serviceCidr: 10.244.0.0/16
        podCidr:
          cidr: 10.44.128.0/17
          public: 10.44.128.0/18
          private: 10.44.192.0/18
      - vpcCidr: 10.45.0.0/16
        subnets:
        - type: public
          cidr: 10.45.0.0/19
        - type: private
          cidr: 10.45.32.0/19
        serviceCidr: 10.243.0.0/16
        podCidr:
          cidr: 10.45.128.0/17
          public: 10.45.128.0/18
          private: 10.45.192.0/18
      - vpcCidr: 10.46.0.0/16
        subnets:
        - type: public
          cidr: 10.46.0.0/19
        - type: private
          cidr: 10.46.32.0/19
        serviceCidr: 10.242.0.0/16
        podCidr:
          cidr: 10.46.128.0/17
          public: 10.46.128.0/18
          private: 10.46.192.0/18
      - vpcCidr: 10.47.0.0/16
        subnets:
        - type: public
          cidr: 10.47.0.0/19
        - type: private
          cidr: 10.47.32.0/19
        serviceCidr: 10.241.0.0/16
        podCidr:
          cidr: 10.47.128.0/17
          public: 10.47.128.0/18
          private: 10.47.192.0/18
      - vpcCidr: 10.48.0.0/16
        subnets:
        - type: public
          cidr: 10.48.0.0/19
        - type: private
          cidr: 10.48.32.0/19
        serviceCidr: 10.240.0.0/16
        podCidr:
          cidr: 10.48.128.0/17
          public: 10.48.128.0/18
          private: 10.48.192.0/18
      - vpcCidr: 10.49.0.0/16
        subnets:
        - type: public
          cidr: 10.49.0.0/19
        - type: private
          cidr: 10.49.32.0/19
        serviceCidr: 10.239.0.0/16
        podCidr:
          cidr: 10.49.128.0/17
          public: 10.49.128.0/18
          private: 10.49.192.0/18
      - vpcCidr: 10.50.0.0/16
        subnets:
        - type: public
          cidr: 10.50.0.0/19
        - type: private
          cidr: 10.50.32.0/19
        serviceCidr: 10.238.0.0/16
        podCidr:
          cidr: 10.50.128.0/17
          public: 10.50.128.0/18
          private: 10.50.192.0/18
      - vpcCidr: 10.51.0.0/16
        subnets:
        - type: public
          cidr: 10.51.0.0/19
        - type: private
          cidr: 10.51.32.0/19
        serviceCidr: 10.237.0.0/16
        podCidr:
          cidr: 10.51.128.0/17
          public: 10.51.128.0/18
          private: 10.51.192.0/18
      - vpcCidr: 10.52.0.0/16
        subnets:
        - type: public
          cidr: 10.52.0.0/19
        - type: private
          cidr: 10.52.32.0/19
        serviceCidr: 10.236.0.0/16
        podCidr:
          cidr: 10.52.128.0/17
          public: 10.52.128.0/18
          private: 10.52.192.0/18
      - vpcCidr: 10.53.0.0/16
        subnets:
        - type: public
          cidr: 10.53.0.0/19
        - type: private
          cidr: 10.53.32.0/19
        serviceCidr: 10.235.0.0/16
        podCidr:
          cidr: 10.53.128.0/17
          public: 10.53.128.0/18
          private: 10.53.192.0/18
      - vpcCidr: 10.54.0.0/16
        subnets:
        - type: public
          cidr: 10.54.0.0/19
        - type: private
          cidr: 10.54.32.0/19
        serviceCidr: 10.234.0.0/16
        podCidr:
          cidr: 10.54.128.0/17
          public: 10.54.128.0/18
          private: 10.54.192.0/18
      - vpcCidr: 10.55.0.0/16
        subnets:
        - type: public
          cidr: 10.55.0.0/19
        - type: private
          cidr: 10.55.32.0/19
        serviceCidr: 10.233.0.0/16
        podCidr:
          cidr: 10.55.128.0/17
          public: 10.55.128.0/18
          private: 10.55.192.0/18
      - vpcCidr: 10.56.0.0/16
        subnets:
        - type: public
          cidr: 10.56.0.0/19
        - type: private
          cidr: 10.56.32.0/19
        serviceCidr: 10.232.0.0/16
        podCidr:
          cidr: 10.56.128.0/17
          public: 10.56.128.0/18
          private: 10.56.192.0/18
      - vpcCidr: 10.57.0.0/16
        subnets:
        - type: public
          cidr: 10.57.0.0/19
        - type: private
          cidr: 10.57.32.0/19
        serviceCidr: 10.231.0.0/16
        podCidr:
          cidr: 10.57.128.0/17
          public: 10.57.128.0/18
          private: 10.57.192.0/18
      - vpcCidr: 10.58.0.0/16
        subnets:
        - type: public
          cidr: 10.58.0.0/19
        - type: private
          cidr: 10.58.32.0/19
        serviceCidr: 10.230.0.0/16
        podCidr:
          cidr: 10.58.128.0/17
          public: 10.58.128.0/18
          private: 10.58.192.0/18
      - vpcCidr: 10.59.0.0/16
        subnets:
        - type: public
          cidr: 10.59.0.0/19
        - type: private
          cidr: 10.59.32.0/19
        serviceCidr: 10.229.0.0/16
        podCidr:
          cidr: 10.59.128.0/17
          public: 10.59.128.0/18
          private: 10.59.192.0/18
      - vpcCidr: 10.60.0.0/16
        subnets:
        - type: public
          cidr: 10.60.0.0/19
        - type: private
          cidr: 10.60.32.0/19
        serviceCidr: 10.228.0.0/16
        podCidr:
          cidr: 10.60.128.0/17
          public: 10.60.128.0/18
          private: 10.60.192.0/18
      - vpcCidr: 10.61.0.0/16
        subnets:
        - type: public
          cidr: 10.61.0.0/19
        - type: private
          cidr: 10.61.32.0/19
        serviceCidr: 10.227.0.0/16
        podCidr:
          cidr: 10.61.128.0/17
          public: 10.61.128.0/18
          private: 10.61.192.0/18
      - vpcCidr: 10.62.0.0/16
        subnets:
        - type: public
          cidr: 10.62.0.0/19
        - type: private
          cidr: 10.62.32.0/19
        serviceCidr: 10.226.0.0/16
        podCidr:
          cidr: 10.62.128.0/17
          public: 10.62.128.0/18
          private: 10.62.192.0/18
      - vpcCidr: 10.63.0.0/16
        subnets:
        - type: public
          cidr: 10.63.0.0/19
        - type: private
          cidr: 10.63.32.0/19
        serviceCidr: 10.225.0.0/16
        podCidr:
          cidr: 10.63.128.0/17
          public: 10.63.128.0/18
          private: 10.63.192.0/18
      - vpcCidr: 10.64.0.0/16
        subnets:
        - type: public
          cidr: 10.64.0.0/19
        - type: private
          cidr: 10.64.32.0/19
        serviceCidr: 10.224.0.0/16
        podCidr:
          cidr: 10.64.128.0/17
          public: 10.64.128.0/18
          private: 10.64.192.0/18
      - vpcCidr: 10.65.0.0/16
        subnets:
        - type: public
          cidr: 10.65.0.0/19
        - type: private
          cidr: 10.65.32.0/19
        serviceCidr: 10.223.0.0/16
        podCidr:
          cidr: 10.65.128.0/17
          public: 10.65.128.0/18
          private: 10.65.192.0/18
      - vpcCidr: 10.66.0.0/16
        subnets:
        - type: public
          cidr: 10.66.0.0/19
        - type: private
          cidr: 10.66.32.0/19
        serviceCidr: 10.222.0.0/16
        podCidr:
          cidr: 10.66.128.0/17
          public: 10.66.128.0/18
          private: 10.66.192.0/18
      - vpcCidr: 10.67.0.0/16
        subnets:
        - type: public
          cidr: 10.67.0.0/19
        - type: private
          cidr: 10.67.32.0/19
        serviceCidr: 10.221.0.0/16
        podCidr:
          cidr: 10.67.128.0/17
          public: 10.67.128.0/18
          private: 10.67.192.0/18
      - vpcCidr: 10.68.0.0/16
        subnets:
        - type: public
          cidr: 10.68.0.0/19
        - type: private
          cidr: 10.68.32.0/19
        serviceCidr: 10.220.0.0/16
        podCidr:
          cidr: 10.68.128.0/17
          public: 10.68.128.0/18
          private: 10.68.192.0/18
      - vpcCidr: 10.69.0.0/16
        subnets:
        - type: public
          cidr: 10.69.0.0/19
        - type: private
          cidr: 10.69.32.0/19
        serviceCidr: 10.219.0.0/16
        podCidr:
          cidr: 10.69.128.0/17
          public: 10.69.128.0/18
          private: 10.69.192.0/18
      - vpcCidr: 10.70.0.0/16
        subnets:
        - type: public
          cidr: 10.70.0.0/19
        - type: private
          cidr: 10.70.32.0/19
        serviceCidr: 10.218.0.0/16
        podCidr:
          cidr: 10.70.128.0/17
          public: 10.70.128.0/18
          private: 10.70.192.0/18
      - vpcCidr: 10.71.0.0/16
        subnets:
        - type: public
          cidr: 10.71.0.0/19
        - type: private
          cidr: 10.71.32.0/19
        serviceCidr: 10.217.0.0/16
        podCidr:
          cidr: 10.71.128.0/17
          public: 10.71.128.0/18
          private: 10.71.192.0/18
      - vpcCidr: 10.72.0.0/16
        subnets:
        - type: public
          cidr: 10.72.0.0/19
        - type: private
          cidr: 10.72.32.0/19
        serviceCidr: 10.216.0.0/16
        podCidr:
          cidr: 10.72.128.0/17
          public: 10.72.128.0/18
          private: 10.72.192.0/18
      - vpcCidr: 10.73.0.0/16
        subnets:
        - type: public
          cidr: 10.73.0.0/19
        - type: private
          cidr: 10.73.32.0/19
        serviceCidr: 10.215.0.0/16
        podCidr:
          cidr: 10.73.128.0/17
          public: 10.73.128.0/18
          private: 10.73.192.0/18
      - vpcCidr: 10.74.0.0/16
        subnets:
        - type: public
          cidr: 10.74.0.0/19
        - type: private
          cidr: 10.74.32.0/19
        serviceCidr: 10.214.0.0/16
        podCidr:
          cidr: 10.74.128.0/17
          public: 10.74.128.0/18
          private: 10.74.192.0/18
      - vpcCidr: 10.75.0.0/16
        subnets:
        - type: public
          cidr: 10.75.0.0/19
        - type: private
          cidr: 10.75.32.0/19
        serviceCidr: 10.213.0.0/16
        podCidr:
          cidr: 10.75.128.0/17
          public: 10.75.128.0/18
          private: 10.75.192.0/18
      - vpcCidr: 10.76.0.0/16
        subnets:
        - type: public
          cidr: 10.76.0.0/19
        - type: private
          cidr: 10.76.32.0/19
        serviceCidr: 10.212.0.0/16
        podCidr:
          cidr: 10.76.128.0/17
          public: 10.76.128.0/18
          private: 10.76.192.0/18
      - vpcCidr: 10.77.0.0/16
        subnets:
        - type: public
          cidr: 10.77.0.0/19
        - type: private
          cidr: 10.77.32.0/19
        serviceCidr: 10.211.0.0/16
        podCidr:
          cidr: 10.77.128.0/17
          public: 10.77.128.0/18
          private: 10.77.192.0/18
      - vpcCidr: 10.78.0.0/16
        subnets:
        - type: public
          cidr: 10.78.0.0/19
        - type: private
          cidr: 10.78.32.0/19
        serviceCidr: 10.210.0.0/16
        podCidr:
          cidr: 10.78.128.0/17
          public: 10.78.128.0/18
          private: 10.78.192.0/18
      - vpcCidr: 10.79.0.0/16
        subnets:
        - type: public
          cidr: 10.79.0.0/19
        - type: private
          cidr: 10.79.32.0/19
        serviceCidr: 10.209.0.0/16
        podCidr:
          cidr: 10.79.128.0/17
          public: 10.79.128.0/18
          private: 10.79.192.0/18
      - vpcCidr: 10.80.0.0/16
        subnets:
        - type: public
          cidr: 10.80.0.0/19
        - type: private
          cidr: 10.80.32.0/19
        serviceCidr: 10.208.0.0/16
        podCidr:
          cidr: 10.80.128.0/17
          public: 10.80.128.0/18
          private: 10.80.192.0/18
      - vpcCidr: 10.81.0.0/16
        subnets:
        - type: public
          cidr: 10.81.0.0/19
        - type: private
          cidr: 10.81.32.0/19
        serviceCidr: 10.207.0.0/16
        podCidr:
          cidr: 10.81.128.0/17
          public: 10.81.128.0/18
          private: 10.81.192.0/18
      - vpcCidr: 10.82.0.0/16
        subnets:
        - type: public
          cidr: 10.82.0.0/19
        - type: private
          cidr: 10.82.32.0/19
        serviceCidr: 10.206.0.0/16
        podCidr:
          cidr: 10.82.128.0/17
          public: 10.82.128.0/18
          private: 10.82.192.0/18
      - vpcCidr: 10.83.0.0/16
        subnets:
        - type: public
          cidr: 10.83.0.0/19
        - type: private
          cidr: 10.83.32.0/19
        serviceCidr: 10.205.0.0/16
        podCidr:
          cidr: 10.83.128.0/17
          public: 10.83.128.0/18
          private: 10.83.192.0/18
      - vpcCidr: 10.84.0.0/16
        subnets:
        - type: public
          cidr: 10.84.0.0/19
        - type: private
          cidr: 10.84.32.0/19
        serviceCidr: 10.204.0.0/16
        podCidr:
          cidr: 10.84.128.0/17
          public: 10.84.128.0/18
          private: 10.84.192.0/18
      - vpcCidr: 10.85.0.0/16
        subnets:
        - type: public
          cidr: 10.85.0.0/19
        - type: private
          cidr: 10.85.32.0/19
        serviceCidr: 10.203.0.0/16
        podCidr:
          cidr: 10.85.128.0/17
          public: 10.85.128.0/18
          private: 10.85.192.0/18
      - vpcCidr: 10.86.0.0/16
        subnets:
        - type: public
          cidr: 10.86.0.0/19
        - type: private
          cidr: 10.86.32.0/19
        serviceCidr: 10.202.0.0/16
        podCidr:
          cidr: 10.86.128.0/17
          public: 10.86.128.0/18
          private: 10.86.192.0/18
      - vpcCidr: 10.87.0.0/16
        subnets:
        - type: public
          cidr: 10.87.0.0/19
        - type: private
          cidr: 10.87.32.0/19
        serviceCidr: 10.201.0.0/16
        podCidr:
          cidr: 10.87.128.0/17
          public: 10.87.128.0/18
          private: 10.87.192.0/18
      - vpcCidr: 10.88.0.0/16
        subnets:
        - type: public
          cidr: 10.88.0.0/19
        - type: private
          cidr: 10.88.32.0/19
        serviceCidr: 10.200.0.0/16
        podCidr:
          cidr: 10.88.128.0/17
          public: 10.88.128.0/18
          private: 10.88.192.0/18
      - vpcCidr: 10.89.0.0/16
        subnets:
        - type: public
          cidr: 10.89.0.0/19
        - type: private
          cidr: 10.89.32.0/19
        serviceCidr: 10.199.0.0/16
        podCidr:
          cidr: 10.89.128.0/17
          public: 10.89.128.0/18
          private: 10.89.192.0/18
      - vpcCidr: 10.90.0.0/16
        subnets:
        - type: public
          cidr: 10.90.0.0/19
        - type: private
          cidr: 10.90.32.0/19
        serviceCidr: 10.198.0.0/16
        podCidr:
          cidr: 10.90.128.0/17
          public: 10.90.128.0/18
          private: 10.90.192.0/18
      - vpcCidr: 10.91.0.0/16
        subnets:
        - type: public
          cidr: 10.91.0.0/19
        - type: private
          cidr: 10.91.32.0/19
        serviceCidr: 10.197.0.0/16
        podCidr:
          cidr: 10.91.128.0/17
          public: 10.91.128.0/18
          private: 10.91.192.0/18
      - vpcCidr: 10.92.0.0/16
        subnets:
        - type: public
          cidr: 10.92.0.0/19
        - type: private
          cidr: 10.92.32.0/19
        serviceCidr: 10.196.0.0/16
        podCidr:
          cidr: 10.92.128.0/17
          public: 10.92.128.0/18
          private: 10.92.192.0/18
      - vpcCidr: 10.93.0.0/16
        subnets:
        - type: public
          cidr: 10.93.0.0/19
        - type: private
          cidr: 10.93.32.0/19
        serviceCidr: 10.195.0.0/16
        podCidr:
          cidr: 10.93.128.0/17
          public: 10.93.128.0/18
          private: 10.93.192.0/18
      - vpcCidr: 10.94.0.0/16
        subnets:
        - type: public
          cidr: 10.94.0.0/19
        - type: private
          cidr: 10.94.32.0/19
        serviceCidr: 10.194.0.0/16
        podCidr:
          cidr: 10.94.128.0/17
          public: 10.94.128.0/18
          private: 10.94.192.0/18
      - vpcCidr: 10.95.0.0/16
        subnets:
        - type: public
          cidr: 10.95.0.0/19
        - type: private
          cidr: 10.95.32.0/19
        serviceCidr: 10.193.0.0/16
        podCidr:
          cidr: 10.95.128.0/17
          public: 10.95.128.0/18
          private: 10.95.192.0/18`,
		"gcp": `
      - vpcCidr: 10.16.0.0/16
        subnets:
        - cidr: 10.16.0.0/18
        nodeCidr: 10.16.128.0/17
        podCidr:
          cidr: 172.16.0.0/16
      - vpcCidr: 10.17.0.0/16
        subnets:
        - cidr: 10.17.0.0/18
        nodeCidr: 10.17.128.0/17
        podCidr:
          cidr: 172.17.0.0/16
      - vpcCidr: 10.18.0.0/16
        subnets:
        - cidr: 10.18.0.0/18
        nodeCidr: 10.18.128.0/17
        podCidr:
          cidr: 172.18.0.0/16
      - vpcCidr: 10.19.0.0/16
        subnets:
        - cidr: 10.19.0.0/18
        nodeCidr: 10.19.128.0/17
        podCidr:
          cidr: 172.19.0.0/16
      - vpcCidr: 10.20.0.0/16
        subnets:
        - cidr: 10.20.0.0/18
        nodeCidr: 10.20.128.0/17
        podCidr:
          cidr: 172.20.0.0/16
      - vpcCidr: 10.21.0.0/16
        subnets:
        - cidr: 10.21.0.0/18
        nodeCidr: 10.21.128.0/17
        podCidr:
          cidr: 172.21.0.0/16
      - vpcCidr: 10.22.0.0/16
        subnets:
        - cidr: 10.22.0.0/18
        nodeCidr: 10.22.128.0/17
        podCidr:
          cidr: 172.22.0.0/16
      - vpcCidr: 10.24.0.0/16
        subnets:
        - cidr: 10.24.0.0/18
        nodeCidr: 10.24.128.0/17
        podCidr:
          cidr: 172.24.0.0/16
      - vpcCidr: 10.25.0.0/16
        subnets:
        - cidr: 10.25.0.0/18
        nodeCidr: 10.25.128.0/17
        podCidr:
          cidr: 172.25.0.0/16
      - vpcCidr: 10.26.0.0/16
        subnets:
        - cidr: 10.26.0.0/18
        nodeCidr: 10.26.128.0/17
        podCidr:
          cidr: 172.26.0.0/16
      - vpcCidr: 10.27.0.0/16
        subnets:
        - cidr: 10.27.0.0/18
        nodeCidr: 10.27.128.0/17
        podCidr:
          cidr: 172.27.0.0/16
      - vpcCidr: 10.28.0.0/16
        subnets:
        - cidr: 10.28.0.0/18
        nodeCidr: 10.28.128.0/17
        podCidr:
          cidr: 172.28.0.0/16
      - vpcCidr: 10.29.0.0/16
        subnets:
        - cidr: 10.29.0.0/18
        nodeCidr: 10.29.128.0/17
        podCidr:
          cidr: 172.29.0.0/16
      - vpcCidr: 10.30.0.0/16
        subnets:
        - cidr: 10.30.0.0/18
        nodeCidr: 10.30.128.0/17
        podCidr:
          cidr: 172.30.0.0/16
      - vpcCidr: 10.31.0.0/16
        subnets:
        - cidr: 10.31.0.0/18
        nodeCidr: 10.31.128.0/17
        podCidr:
          cidr: 172.31.0.0/16
      - vpcCidr: 10.32.0.0/16
        subnets:
        - cidr: 10.32.0.0/18
        nodeCidr: 10.32.128.0/17
        podCidr:
          cidr: 172.32.0.0/16`,
		"openstack": `
      - vpcCidr: 10.82.0.0/17
        subnets:
        - cidr: 10.82.0.0/18
        serviceCidr: 10.82.192.0/18
        podCidr:
          cidr: 10.82.64.0/18
      - vpcCidr: 10.83.0.0/17
        subnets:
        - cidr: 10.83.0.0/18
        serviceCidr: 10.83.192.0/18
        podCidr:
          cidr: 10.83.64.0/18
      - vpcCidr: 10.84.0.0/17
        subnets:
        - cidr: 10.84.0.0/18
        serviceCidr: 10.84.192.0/18
        podCidr:
          cidr: 10.84.64.0/18
      - vpcCidr: 10.85.0.0/17
        subnets:
        - cidr: 10.85.0.0/18
        serviceCidr: 10.85.192.0/18
        podCidr:
          cidr: 10.85.64.0/18
      - vpcCidr: 10.86.0.0/17
        subnets:
        - cidr: 10.86.0.0/18
        serviceCidr: 10.86.192.0/18
        podCidr:
          cidr: 10.86.64.0/18
      - vpcCidr: 10.87.0.0/17
        subnets:
        - cidr: 10.87.0.0/18
        serviceCidr: 10.87.192.0/18
        podCidr:
          cidr: 10.87.64.0/18
      - vpcCidr: 10.88.0.0/17
        subnets:
        - cidr: 10.88.0.0/18
        serviceCidr: 10.88.192.0/18
        podCidr:
          cidr: 10.88.64.0/18
      - vpcCidr: 10.89.0.0/17
        subnets:
        - cidr: 10.89.0.0/18
        serviceCidr: 10.89.192.0/18
        podCidr:
          cidr: 10.89.64.0/18
      - vpcCidr: 10.90.0.0/17
        subnets:
        - cidr: 10.90.0.0/18
        serviceCidr: 10.90.192.0/18
        podCidr:
          cidr: 10.90.64.0/18
      - vpcCidr: 10.91.0.0/17
        subnets:
        - cidr: 10.91.0.0/18
        serviceCidr: 10.91.192.0/18
        podCidr:
          cidr: 10.91.64.0/18`,
	}
}

func configMapData(zone string) map[string]string {
	return map[string]string{
		"flavors.yaml": `- zone: ` + zone + `
  zoneOfferings:
  - name: m3.xlarge
    nameLabel: 2vCPU-4GB
    vcpus: 2
    ram: 4GB
    price: "0.05"
    gpu:
      enabled: false
    spot:
      price: "0.02"
      enabled: true
  - name: g5.12xlarge
    nameLabel: 48vCPU-192GB-4xA10G-22GB
    vcpus: 48
    ram: 192GB
    price: "5.67"
    gpu:
      enabled: true
      manufacturer: NVIDIA
      count: 4
      model: A10G
      memory: 22GB
    spot:
      price: "2.12"
      enabled: true
  - name: g5.16xlarge
    nameLabel: 64vCPU-256GB-1xA10G-22GB
    vcpus: 64
    ram: 256GB
    price: "4.10"
    gpu:
      enabled: true
      manufacturer: NVIDIA
      count: 1
      model: A10G
      memory: 22GB
    spot:
      price: "1.07"
      enabled: true
  - name: g2-standard-12   
    nameLabel: 12vCPU-49GB-1xL4-24GB
    vcpus: 12
    ram: 49GB
    price: "$0.4893"
    gpu:
      enabled: true
      manufacturer: NVIDIA
      count: 1
      model: L4
      memory: 24GB
    spot:
      price: "$0.1365"
      enabled: true`,
		"images.yaml": `- nameLabel: ubuntu-20.04
name: ami-0fb0b230890ccd1e6
zone: ` + zone + `
- nameLabel: ubuntu-22.04
name: ami-0e70225fadb23da91
zone: ` + zone + `
- nameLabel: ubuntu-24.04
name: ami-07033cb190109bd1d
zone: ` + zone,
		"managed-k8s.yaml": `- name: EKS
nameLabel: ManagedKubernetes
overhead:
  cost: "0.096"
  count: 1
  instanceType: m5.xlarge
price: "0.10"`,
	}
}

// returns the provider profile and a boolean indicating if it was created
func createProviderProfileAWS(typeNamespacedName types.NamespacedName) *cv1a1.ProviderProfile {
	return &cv1a1.ProviderProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      typeNamespacedName.Name,
			Namespace: typeNamespacedName.Namespace,
		},
		Spec: cv1a1.ProviderProfileSpec{
			Platform:    "aws",
			RegionAlias: "us-east",
			Region:      "us-east-1",
			Zones: []cv1a1.ZoneSpec{
				{Name: "us-east-1a", Enabled: true, DefaultZone: true, Type: "cloud"},
				{Name: "us-east-1b", Enabled: true, Type: "cloud"},
			},
		},
	}
}

func createProviderProfileGCP(typeNamespacedName types.NamespacedName) *cv1a1.ProviderProfile {
	return &cv1a1.ProviderProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      typeNamespacedName.Name,
			Namespace: typeNamespacedName.Namespace,
		},
		Spec: cv1a1.ProviderProfileSpec{
			Platform:    "gcp",
			RegionAlias: "us-east",
			Region:      "us-east1",
			Zones: []cv1a1.ZoneSpec{
				{Name: "us-east1-a", Enabled: true, DefaultZone: true, Type: "cloud"},
				{Name: "us-east1-b", Enabled: true, Type: "cloud"},
			},
		},
	}
}

func getNewXSetup(resourceName string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"apiServer": "140.140.140.140:6443",
				"submariner": map[string]any{
					"enabled": true,
				},
			},
		},
	}
	obj.SetAPIVersion("skycluster.io/v1alpha1")
	obj.SetKind("XSetup")
	obj.SetName(resourceName)
	return obj
}

func getXSetupStatus() map[string]any {
	return map[string]any{
		"apiServer": "140.140.140.140:6443",
		"namespace": "default",
		"ca": map[string]any{
			"certificate":     "LS0tLUdJTLS0tCg==",
			"secretName":      "skycluster-self-ca",
			"secretNamespace": "skycluster-system",
		},
		"conditions": []any{
			map[string]any{
				"type":               "Synced",
				"status":             "True",
				"reason":             "ReconcileSuccess",
				"lastTransitionTime": "2025-11-28T04:45:34Z",
			},
			map[string]any{
				"type":               "Ready",
				"status":             "True",
				"reason":             "Available",
				"lastTransitionTime": "2025-11-28T04:50:41Z",
			},
		},
		"providerConfig": map[string]any{
			"helm": map[string]any{
				"name": "mycluster-7hzcx",
			},
			"kubernetes": map[string]any{
				"name": "mycluster-xn5pm",
			},
		},
		"submariner": map[string]any{
			"connectionSecretName": "submariner-connection-secret",
		},
		"headscale": map[string]any{
			"connectionSecretName": "headscale-connection-secret",
			"loginUrl":             "https://140.140.140.140:8080",
			"token":                "5ff34c13e34cd9cb86c5a0",
		},
		"istio": map[string]any{
			"rootCASecretName": "istio-root-ca",
		},
		"keypair": map[string]any{
			"publicKey":       "ssh-rsa AAAAB3NzaC1yc2+tKHnQ== ubuntu@skycluster-dev",
			"secretName":      "skycluster-keys",
			"secretNamespace": "skycluster-system",
		},
	}
}

func getXKubeStatus() map[string]any {
	return map[string]any{
		"log":                 "log",
		"serviceCidr":         "10.0.0.0/24",
		"podCidr":             "10.0.1.0/24",
		"clusterName":         "name",
		"externalClusterName": "name",
		"clusterSecretName":   "cluster-secret",
		"providerConfigs": map[string]any{
			"k8s":  "example-kube-os-scinet-mcwbq",
			"helm": "helm-openstack-scinet-10.135.0.23",
		},
		"controllers": []any{
			map[string]any{
				"privateIp": "privateIp",
				"publicIp":  "publicIp",
			},
		},
		"agents": []any{
			map[string]any{
				"privateIp":    "privateIp",
				"publicIp":     "publicIp",
				"publicAccess": true,
			},
		},
	}
}

func createXKube(platform, resourceName string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"applicationId": "example-app",
				"nodeCidr":      "10.0.2.0/24",
				"serviceCidr":   "10.0.0.0/24",
				"podCidr": map[string]any{
					"cidr":    "10.0.1.0/24",
					"public":  "",
					"private": "",
				},
				"controlPlane": map[string]any{
					"deviceNodeName": "example-gw-node",
					"instanceType":   "8vCPU-32GB",
					"autoScaling": map[string]any{
						"enabled": true,
						"minSize": 1,
						"maxSize": 3,
					},
					"highAvailability": true,
				},
				"nodes": []any{
					"example-node-1",
				},
				"nodeGroups": []any{
					map[string]any{
						"nodeCount":    1,
						"instanceType": "4vCPU-4GB",
						"instanceTypes": []any{
							"4vCPU-4GB",
						},
						"type":         "default",
						"publicAccess": true,
						"autoScaling": map[string]any{
							"enabled": false,
							"minSize": 1,
							"maxSize": 1,
						},
					},
				},
				"principal": map[string]any{
					"type": "user",
					"id":   "string",
				},
				"providerRef": func() map[string]any {
					if platform == "aws" {
						return map[string]any{
							"platform": platform,
							"region":   "us-east-1",
							"zones": map[string]any{
								"primary":   "us-east-1a",
								"secondary": "us-east-1b",
							},
						}
					}
					if platform == "gcp" {
						return map[string]any{
							"platform": platform,
							"region":   "us-east1",
							"zones": map[string]any{
								"primary": "us-east1-a",
							},
						}
					}
					return map[string]any{} // return empty map if platform is not aws or gcp
				}(),
			},
		},
	}
	obj.SetAPIVersion("skycluster.io/v1alpha1")
	obj.SetKind("XKube")
	obj.SetName(resourceName)
	return obj
}
