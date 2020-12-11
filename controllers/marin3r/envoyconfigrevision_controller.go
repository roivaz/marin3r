/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	marin3rv1alpha1 "github.com/3scale/marin3r/apis/marin3r/v1alpha1"
	envoyconfigrevision "github.com/3scale/marin3r/controllers/marin3r/envoyconfigrevision"
	"github.com/3scale/marin3r/pkg/common"
	xdss "github.com/3scale/marin3r/pkg/discoveryservice/xdss"
	envoy "github.com/3scale/marin3r/pkg/envoy"
	envoy_resources "github.com/3scale/marin3r/pkg/envoy/resources"
	envoy_serializer "github.com/3scale/marin3r/pkg/envoy/serializer"
	reconcilers_marin3r "github.com/3scale/marin3r/pkg/reconcilers/marin3r"

	"github.com/go-logr/logr"
	"github.com/operator-framework/operator-lib/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// EnvoyConfigRevisionReconciler reconciles a EnvoyConfigRevision object
type EnvoyConfigRevisionReconciler struct {
	Client     client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	XdsCache   xdss.Cache
	APIVersion envoy.APIVersion
}

// +kubebuilder:rbac:groups=marin3r.3scale.net,namespace=placeholder,resources=envoyconfigrevisions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=marin3r.3scale.net,namespace=placeholder,resources=envoyconfigrevisions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="core",namespace=placeholder,resources=secrets,verbs=get;list;watch

func (r *EnvoyConfigRevisionReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("name", req.Name, "namespace", req.Namespace)

	// Fetch the EnvoyConfigRevision instance
	ecr := &marin3rv1alpha1.EnvoyConfigRevision{}
	err := r.Client.Get(ctx, req.NamespacedName, ecr)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	if ok := envoyconfigrevision.IsInitialized(ecr); !ok {
		err := r.Client.Update(ctx, ecr)
		if err != nil {
			log.Error(err, "unable to update EnvoyConfigRevision")
			return ctrl.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	if common.IsBeingDeleted(ecr) {
		if !controllerutil.ContainsFinalizer(ecr, marin3rv1alpha1.EnvoyConfigRevisionFinalizer) {
			return reconcile.Result{}, nil
		}
		envoyconfigrevision.CleanupLogic(ecr, r.XdsCache, log)
		controllerutil.RemoveFinalizer(ecr, marin3rv1alpha1.EnvoyConfigRevisionFinalizer)
		if err = r.Client.Update(ctx, ecr); err != nil {
			log.Error(err, "unable to update EnvoyConfigRevision")
			return reconcile.Result{}, nil
		}
	}

	// If this ecr has the RevisionPublishedCondition set to "True" pusblish the resources
	// to the xds server cache
	if ecr.Status.Conditions.IsTrueFor(marin3rv1alpha1.RevisionPublishedCondition) {
		decoder := envoy_serializer.NewResourceUnmarshaller(ecr.GetSerialization(), r.APIVersion)

		cacheReconciler := reconcilers_marin3r.NewCacheReconciler(
			ctx, r.Log, r.Client, r.XdsCache,
			decoder,
			envoy_resources.NewGenerator(r.APIVersion),
		)

		result, err := cacheReconciler.Reconcile(req.NamespacedName, ecr.Spec.EnvoyResources, ecr.Spec.NodeID, ecr.Spec.Version)

		// If a type errors.StatusError is returned it means that the config in spec.envoyResources is wrong
		// and cannot be written into the xDS cache. This is true for any error loading all types of resources
		// except for Secrets. Secrets are dynamically loaded from the API and transient failures are possible, so
		// setting a permanent taint could occur for a transient failure, which is not desirable.
		if result.Requeue || err != nil {
			switch err.(type) {
			case *errors.StatusError:
				if err := r.taintSelf(ctx, ecr, "FailedLoadingResources", err.Error()); err != nil {
					return ctrl.Result{}, err
				}
			default:
				return result, err
			}
		}
	}

	// Update status
	if err := r.updateStatus(ctx, ecr); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *EnvoyConfigRevisionReconciler) updateStatus(ctx context.Context, ecr *marin3rv1alpha1.EnvoyConfigRevision) error {

	changed := false
	patch := client.MergeFrom(ecr.DeepCopy())

	// Clear ResourcesOutOfSyncCondition
	if ecr.Status.Conditions.IsTrueFor(marin3rv1alpha1.ResourcesOutOfSyncCondition) {
		ecr.Status.Conditions.SetCondition(status.Condition{
			Type:    marin3rv1alpha1.ResourcesOutOfSyncCondition,
			Reason:  "NodeConficRevisionSynced",
			Status:  corev1.ConditionFalse,
			Message: "EnvoyConfigRevision successfully synced",
		})
		changed = true

	}

	// Set status.published and status.lastPublishedAt fields
	if ecr.Status.Conditions.IsTrueFor(marin3rv1alpha1.RevisionPublishedCondition) && !*ecr.Status.Published {
		ecr.Status.Published = pointer.BoolPtr(true)
		ecr.Status.LastPublishedAt = func(t metav1.Time) *metav1.Time { return &t }(metav1.Now())
		// We also initialise the "tainted" status property to false
		ecr.Status.Tainted = pointer.BoolPtr(false)
		changed = true
	} else if !ecr.Status.Conditions.IsTrueFor(marin3rv1alpha1.RevisionPublishedCondition) && *ecr.Status.Published {
		ecr.Status.Published = pointer.BoolPtr(false)
		changed = true
	}

	// Set status.failed field
	if ecr.Status.Conditions.IsTrueFor(marin3rv1alpha1.RevisionTaintedCondition) && !*ecr.Status.Tainted {
		ecr.Status.Tainted = pointer.BoolPtr(true)
		changed = true
	} else if !ecr.Status.Conditions.IsTrueFor(marin3rv1alpha1.RevisionTaintedCondition) && *ecr.Status.Tainted {
		ecr.Status.Tainted = pointer.BoolPtr(false)
		changed = true
	}

	if changed {
		if err := r.Client.Status().Patch(ctx, ecr, patch); err != nil {
			return err
		}
	}

	return nil
}

func (r *EnvoyConfigRevisionReconciler) taintSelf(ctx context.Context, ecr *marin3rv1alpha1.EnvoyConfigRevision, reason, msg string) error {
	if !ecr.Status.Conditions.IsTrueFor(marin3rv1alpha1.RevisionTaintedCondition) {
		patch := client.MergeFrom(ecr.DeepCopy())
		ecr.Status.Conditions.SetCondition(status.Condition{
			Type:    marin3rv1alpha1.RevisionTaintedCondition,
			Status:  corev1.ConditionTrue,
			Reason:  status.ConditionReason(reason),
			Message: msg,
		})
		ecr.Status.Tainted = pointer.BoolPtr(true)

		if err := r.Client.Status().Patch(ctx, ecr, patch); err != nil {
			return err
		}
	}
	return nil
}

func filterByAPIVersion(obj runtime.Object, version envoy.APIVersion) bool {
	switch o := obj.(type) {
	case *marin3rv1alpha1.EnvoyConfigRevision:
		if o.GetEnvoyAPIVersion() == version {
			return true
		}
		return false

	default:
		return false
	}
}

func filterByAPIVersionPredicate(version envoy.APIVersion,
	filter func(runtime.Object, envoy.APIVersion) bool) predicate.Predicate {

	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return filter(e.Object, version)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return filter(e.ObjectNew, version)

		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return filter(e.Object, version)
		},
	}
}

func (r *EnvoyConfigRevisionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&marin3rv1alpha1.EnvoyConfigRevision{}).
		WithEventFilter(filterByAPIVersionPredicate(r.APIVersion, filterByAPIVersion)).
		Complete(r)
}
