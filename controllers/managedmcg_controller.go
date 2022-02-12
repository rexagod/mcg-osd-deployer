package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	noobaav1alpha1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	odfv1alpha1 "github.com/red-hat-data-services/odf-operator/api/v1alpha1"
	mcgv1alpha1 "github.com/red-hat-storage/mcg-osd-deployer/api/v1alpha1"
	"github.com/red-hat-storage/mcg-osd-deployer/internal"
	mcginternal "github.com/red-hat-storage/mcg-osd-deployer/internal"
	"github.com/red-hat-storage/mcg-osd-deployer/templates"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ManagedMCGName      = "managedmcg"
	ManagedMCGFinalizer = "managedmcg.openshift.io"
	StorageSystemName   = mcginternal.StorageSystemName

	noobaaName                      = "noobaa"
	odfOperatorManagerConfigMapName = "odf-operator-manager-config"
)

// ImageMap holds mapping information between component image name and the image url
type ImageMap struct {
	NooBaaCore string
	NooBaaDB   string
}

// ManagedMCGReconciler reconciles a ManagedMCG object
type ManagedMCGReconciler struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx                         context.Context
	namespace                   string
	managedMCG                  *mcgv1alpha1.ManagedMCG
	odfOperatorManagerConfigMap *corev1.ConfigMap
	noobaa                      *noobaav1alpha1.NooBaa
	images                      ImageMap
	reconcileStrategy           mcgv1alpha1.ReconcileStrategy
	storageSystem               *odfv1alpha1.StorageSystem
}

func (r *ManagedMCGReconciler) initReconciler(req ctrl.Request) {
	r.ctx = context.Background()

	reqName := req.NamespacedName.Name
	reqNamespace := req.NamespacedName.Namespace

	r.namespace = reqNamespace

	r.managedMCG = &mcgv1alpha1.ManagedMCG{}
	r.managedMCG.Name = reqName
	r.managedMCG.Namespace = reqNamespace

	r.odfOperatorManagerConfigMap = &corev1.ConfigMap{}
	r.odfOperatorManagerConfigMap.Name = odfOperatorManagerConfigMapName
	r.odfOperatorManagerConfigMap.Namespace = reqNamespace

	r.noobaa = &noobaav1alpha1.NooBaa{}
	r.noobaa.Name = noobaaName
	r.noobaa.Namespace = reqNamespace

	r.storageSystem = &odfv1alpha1.StorageSystem{}
	r.storageSystem.Name = StorageSystemName
	r.storageSystem.Namespace = reqNamespace
}

//+kubebuilder:rbac:groups=mcg.openshift.io,resources={managedmcg,managedmcg/finalizers},verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcg.openshift.io,resources=managedmcg/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcg.openshift.io,resources={managedmcgs,managedmcgs/finalizers},verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcg.openshift.io,resources=managedmcgs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",namespace=system,resources={secrets,configmaps},verbs=create;get;list;watch;update
//+kubebuilder:rbac:groups="coordination.k8s.io",namespace=system,resources=leases,verbs=create;get;list;watch;update
//+kubebuilder:rbac:groups=operators.coreos.com,namespace=system,resources=clusterserviceversions,verbs=get;list;watch;delete;update;patch
//+kubebuilder:rbac:groups=noobaa.io,namespace=system,resources=noobaas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=odf.openshift.io,namespace=system,resources=storagesystems,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,namespace=system,resources=storageclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="apps",namespace=system,resources=statefulsets,verbs=get;list;watch
//+kubebuilder:rbac:groups="storage.k8s.io",resources=storageclass,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ManagedMCGReconciler) Reconcile(_ context.Context /* maintain the reconciler context instead */, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("namespace", req.Namespace, "name", req.Name)
	log.Info("starting reconcile for ManagedMCG")
	r.initReconciler(req)
	if err := r.get(r.managedMCG); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("ManagedMCG resource not found")
		} else {
			return ctrl.Result{}, err
		}
	}
	result, err := r.reconcilePhases()
	if err != nil {
		r.Log.Error(err, "failed to reconcile")
		// Not returning error here because we want to update managedMCG status first
	}
	// Update status if managedMCG was created successfully
	var statusErr error
	if r.managedMCG.UID != "" {
		statusErr = r.Client.Status().Update(r.ctx, r.managedMCG)
	}
	// Reconcile errors have priority to status update errors
	if err != nil {
		return ctrl.Result{}, err
	} else if statusErr != nil {
		return ctrl.Result{}, statusErr
	} else {
		return result, nil
	}
}

func (r *ManagedMCGReconciler) reconcilePhases() (reconcile.Result, error) {
	r.Log.Info("started reconciling phases")

	err := r.updateComponentStatus()
	if err != nil {
		return reconcile.Result{}, err
	}

	// managedMCG is read-only till finalizers are set and the managedMCG is not deleted
	if !r.managedMCG.DeletionTimestamp.IsZero() {
		r.Log.Info("removing managedMCG resource if DeletionTimestamp exceeded")
		if r.managedMCG.Status.Components.Noobaa.State == mcgv1alpha1.ComponentNotFound {
			r.Log.Info("removing finalizer from the ManagedMCG resource")
			r.managedMCG.SetFinalizers(internal.Remove(r.managedMCG.GetFinalizers(), ManagedMCGFinalizer))
			if err := r.Client.Update(r.ctx, r.managedMCG); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from ManagedMCG: %v", err)
			}
			r.Log.Info("finalizer removed successfully")
		} else {
			// explicitly delete managedMCG components if they exist
			r.Log.Info("deleting noobaa")
			if err := r.delete(r.noobaa); err != nil {
				return ctrl.Result{}, fmt.Errorf("unable to delete noobaa system: %v", err)
			}
		}
	} else if r.managedMCG.UID != "" {
		r.Log.Info("Reconciler phase started with valid UID")
		if !internal.Contains(r.managedMCG.GetFinalizers(), ManagedMCGFinalizer) {
			r.Log.Info("finalizer missing on the managedMCG resource, adding it")
			r.managedMCG.SetFinalizers(append(r.managedMCG.GetFinalizers(), ManagedMCGFinalizer))
			if err := r.update(r.managedMCG); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update managedMCG with finalizer: %v", err)
			}
		}
		r.reconcileStrategy = mcgv1alpha1.ReconcileStrategyStrict
		if strings.EqualFold(string(r.managedMCG.Spec.ReconcileStrategy), string(mcgv1alpha1.ReconcileStrategyNone)) {
			r.reconcileStrategy = mcgv1alpha1.ReconcileStrategyNone
		}
		if err := r.reconcileODFOperatorMgrConfig(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileStorageSystem(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileNoobaa(); err != nil {
			return ctrl.Result{}, err
		}
		r.managedMCG.Status.ReconcileStrategy = r.reconcileStrategy
	}
	return ctrl.Result{}, nil
}

func (r *ManagedMCGReconciler) reconcileNoobaa() error {
	r.Log.Info("Reconciling Noobaa")
	noobaaList := noobaav1alpha1.NooBaaList{}
	if err := r.list(&noobaaList); err == nil {
		for _, noobaa := range noobaaList.Items {
			if noobaa.Name == "noobaa" {
				r.Log.Info("noobaa instance already exists")
				return nil
			}
		}
	}
	r.Log.Info("noobaa instance does not exist, creating it")
	desiredNoobaa := templates.NoobaaTemplate.DeepCopy()
	r.setNooBaaDesiredState(desiredNoobaa)
	// NOTE MutateFn is called before the object is created or updated
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.noobaa, func() error {
		if err := r.own(r.storageSystem); err != nil {
			return err
		}
		r.noobaa.Spec = desiredNoobaa.Spec
		return nil
	})
	return err
}

func (r *ManagedMCGReconciler) setNooBaaDesiredState(desiredNoobaa *noobaav1alpha1.NooBaa) {
	coreResources := internal.GetDaemonResources("noobaa-core")
	dbResources := internal.GetDaemonResources("noobaa-db")
	dBVolumeResources := internal.GetDaemonResources("noobaa-db-vol")
	endpointResources := internal.GetDaemonResources("noobaa-endpoint")
	desiredNoobaa.Labels = map[string]string{
		"app": "noobaa",
	}
	desiredNoobaa.Spec.CoreResources = &coreResources
	desiredNoobaa.Spec.DBResources = &dbResources
	desiredNoobaa.Spec.DBVolumeResources = &dBVolumeResources
	desiredNoobaa.Spec.Image = &r.images.NooBaaCore
	desiredNoobaa.Spec.DBImage = &r.images.NooBaaDB
	desiredNoobaa.Spec.DBType = noobaav1alpha1.DBTypePostgres
	desiredNoobaa.Spec.Endpoints = &noobaav1alpha1.EndpointsSpec{
		MinCount:               1,
		MaxCount:               2,
		AdditionalVirtualHosts: []string{},
		Resources:              &endpointResources,
	}
}

func (r *ManagedMCGReconciler) reconcileStorageSystem() error {
	r.Log.Info("reconciling StorageSystem")
	ssList := odfv1alpha1.StorageSystemList{}
	if err := r.list(&ssList); err == nil {
		for _, storageSystem := range ssList.Items {
			if storageSystem.Name == StorageSystemName {
				r.Log.Info("storageSystem instance already exists")
				return nil
			}
		}
	}
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.storageSystem, func() error {
		if r.reconcileStrategy == mcgv1alpha1.ReconcileStrategyStrict {
			desiredStorageSystem := templates.StorageSystemTemplate.DeepCopy()
			r.storageSystem.Spec = desiredStorageSystem.Spec
		}
		return nil
	})
	return err
}

func (r *ManagedMCGReconciler) reconcileODFOperatorMgrConfig() error {
	r.Log.Info("Reconciling odf-operator-manager-config ConfigMap")
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.odfOperatorManagerConfigMap, func() error {
		r.odfOperatorManagerConfigMap.Data["ODF_SUBSCRIPTION_NAME"] = "odf-operator-stable-4.9-redhat-operators-openshift-marketplace"
		r.odfOperatorManagerConfigMap.Data["NOOBAA_SUBSCRIPTION_STARTINGCSV"] = "mcg-operator.v4.9.2"
		return nil
	})
	return err
}

func (r *ManagedMCGReconciler) updateComponentStatus() error {
	r.Log.Info("updating component status")
	noobaa := &r.managedMCG.Status.Components.Noobaa
	if err := r.get(r.noobaa); err == nil {
		if r.noobaa.Status.Phase == "Ready" {
			noobaa.State = mcgv1alpha1.ComponentReady
		} else {
			noobaa.State = mcgv1alpha1.ComponentPending
		}
	} else if errors.IsNotFound(err) {
		noobaa.State = mcgv1alpha1.ComponentNotFound
	} else {
		noobaa.State = mcgv1alpha1.ComponentUnknown
	}
	statusErr := r.Client.Status().Update(r.ctx, r.managedMCG)
	return statusErr
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedMCGReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.lookupNoobaaImages(); err != nil {
		return err
	}
	managedMCGPredicates := builder.WithPredicates(
		predicate.GenerationChangedPredicate{},
	)
	configMapPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(client client.Object) bool {
				name := client.GetName()
				if name == odfOperatorManagerConfigMapName {
					return true
				}
				return false
			},
		),
	)
	enqueueManagedMCGRequest := handler.EnqueueRequestsFromMapFunc(
		func(client client.Object) []reconcile.Request {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      ManagedMCGName,
					Namespace: client.GetNamespace(),
				},
			}}
		},
	)
	return ctrl.NewControllerManagedBy(mgr).
		// For defines the type of Object being *reconciled*, and configures the ControllerManagedBy to respond to
		// create / delete / update events by *reconciling the object*. This is the equivalent of calling Watches(&source.Kind{Type: apiType}, &handler.EnqueueRequestForObject{})
		For(&mcgv1alpha1.ManagedMCG{}, managedMCGPredicates).
		// Owns method defines types of Objects being *generated* by the ControllerManagedBy, and configures the
		// ControllerManagedBy to respond to create / delete / update events by *reconciling the owner object*.
		// This is the equivalent of calling Watches(&source.Kind{Type: <ForType-forInput>}, &handler.EnqueueRequestForOwner{OwnerType: apiType, IsController: true}).
		Watches(
			&source.Kind{Type: &corev1.ConfigMap{}},
			enqueueManagedMCGRequest,
			configMapPredicates,
		).
		Watches(
			&source.Kind{Type: &odfv1alpha1.StorageSystem{}},
			enqueueManagedMCGRequest,
		).
		Watches(
			&source.Kind{Type: &noobaav1alpha1.NooBaa{}},
			enqueueManagedMCGRequest,
		).
		Complete(r)
}
