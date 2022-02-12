package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/go-logr/logr"
	noobaa "github.com/noobaa/noobaa-operator/v5/pkg/apis"
	odfv1alpha1 "github.com/red-hat-data-services/odf-operator/api/v1alpha1"
	mcgv1alpha1 "github.com/red-hat-storage/mcg-osd-deployer/api/v1alpha1"
	"github.com/red-hat-storage/mcg-osd-deployer/controllers"
	mcginternal "github.com/red-hat-storage/mcg-osd-deployer/internal"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	namespaceEnvVarKey = mcginternal.NamespaceEnvVarKey
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(ocsv1.AddToScheme(scheme))

	utilruntime.Must(odfv1alpha1.AddToScheme(scheme))

	utilruntime.Must(mcgv1alpha1.AddToScheme(scheme))

	utilruntime.Must(noobaa.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	namespaceEnvVarValue, found := mcginternal.LookupEnvVar(namespaceEnvVarKey)
	if !found {
		setupLog.Error(fmt.Errorf("%s environment variable not found", namespaceEnvVarKey), "failed to get namespace")
		os.Exit(1)
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "af4bf43b.mcg.openshift.io",
		Namespace:          namespaceEnvVarValue,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager.")
		os.Exit(1)
	}

	if err = (&controllers.ManagedMCGReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("ManagedMCG"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ManagedMCG")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := ensureManagedMCG(context.Background(), mgr.GetClient(), setupLog, namespaceEnvVarValue); err != nil {
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func ensureManagedMCG(ctx context.Context, c client.Client, log logr.Logger, namespace string) error {
	err := c.Create(ctx, &mcgv1alpha1.ManagedMCG{
		ObjectMeta: metav1.ObjectMeta{
			Name:       controllers.ManagedMCGName,
			Namespace:  namespace,
			Finalizers: []string{controllers.ManagedMCGFinalizer},
		},
	})
	if err == nil {
		log.Info("ManagedMCG resource created.")
		return nil
	} else if errors.IsAlreadyExists(err) {
		log.Info("ManagedMCG resource already exists.")
		return nil
	} else {
		log.Error(err, "unable to create ManagedMCG resource")
		return err
	}
}
