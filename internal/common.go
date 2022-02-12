package internal

import (
	corev1 "k8s.io/api/core/v1"
	"os"
)

const (
	NamespaceEnvVarKey = "MCG_OSD_DEPLOYER_NAMESPACE"
	StorageSystemName  = "mcg-storagecluster-storagesystem"
)

func GetDaemonResources(name string) corev1.ResourceRequirements {
	return DaemonResources[name]
}

// Contains checks whether a string is present within a slice
func Contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// Remove eliminates a given string from a slice and returns the new slice
func Remove(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

// LookupEnvVar checks for and fetches the given environment variable
func LookupEnvVar(s string) (string, bool) {
	v, ok := os.LookupEnv(s)
	return v, ok
}
