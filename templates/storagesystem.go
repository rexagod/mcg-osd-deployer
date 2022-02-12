package templates

import (
	mcginternal "github.com/red-hat-storage/mcg-osd-deployer/internal"
	"reflect"
	"strings"

	odfv1alpha1 "github.com/red-hat-data-services/odf-operator/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var mcgOsdDeployerNamespace, _ = mcginternal.LookupEnvVar(mcginternal.NamespaceEnvVarKey)

var storageClusterKind = odfv1alpha1.StorageKind(strings.ToLower(reflect.TypeOf(ocsv1.StorageCluster{}).Name()) + "." + ocsv1.GroupVersion.String())

var StorageSystemTemplate = &odfv1alpha1.StorageSystem{
	ObjectMeta: metav1.ObjectMeta{
		Name:      mcginternal.StorageSystemName,
		Namespace: mcgOsdDeployerNamespace,
	},
	Spec: odfv1alpha1.StorageSystemSpec{
		Name:      mcginternal.StorageSystemName,
		Namespace: mcgOsdDeployerNamespace,
		Kind:      storageClusterKind,
	},
}
