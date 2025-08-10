/*
Copyright 2025.

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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1alpha1 "github.com/guojiaxua/api/v1alpha1"
)

const finalizer = "finalizer.csi.k8s.io"

var logger = log.Log.WithName("ScalerController")

var originalDeploymentInfo = make(map[string]apiv1alpha1.DeploymentInfo)

var annotations = make(map[string]string)

// ScalerReconciler reconciles a Scaler object
type ScalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=api.operator.com,resources=scalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=api.operator.com,resources=scalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=api.operator.com,resources=scalers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Scaler object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ScalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logger.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	log.Info("Reconcile called")

	//Build a scaler instance
	scaler := &apiv1alpha1.Scaler{}
	err := r.Get(ctx, req.NamespacedName, scaler)
	if err != nil {
		//If we cannot find this scaler instance, we ignore the error let the process still run
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if scaler.Status.Status == "" {
		scaler.Status.Status = apiv1alpha1.PENDING
		err := r.Status().Update(ctx, scaler)
		if err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		if scaler.ObjectMeta.DeletionTimestemp.IsZero != nil {
			if !controllerutil.ContainsFinalizer(scaler, finalizer) {

				controllerutil.AddFinalizer(scaler, finalizer)
				log.Info("finalizer added", finalizer)
				err := r.Update(ctx, scaler)
				if err != nil {
					return ctrl.Result{}, client.IgnoreNotFound(err)
				}
			}
			if scaler.Status.Status == "" {
				scaler.Status.Status = apiv1alpha1.PENDING
				err := r.Status().Update(ctx, scaler)
				if err != nil {
					return ctrl.Result{}, client.IgnoreNotFound(err)
				}

				if scaler.ObjectMeta.DeletionTimestemp.IsZero != nil {
					if !controllerutil.ContainsFinalizer(scaler, finalizer) {

						controllerutil.AddFinalizer(scaler, finalizer)
						log.Info("finalizer added", finalizer)
						err := r.Update(ctx, scaler)
						if err != nil {
							return ctrl.Result{}, client.IgnoreNotFound(err)
						}
					}

				}

				//Add the number of replicas and namespace which managed by scaler to annotations
				if err := addAnnotations(scaler, r, ctx); err != nil {
					return ctrl.Result{}, client.IgnoreNotFound(err)
				}
			}

			startTime := scaler.Spec.Start
			endTime := scaler.Spec.End
			replicas := scaler.Spec.Replicas

			currentHour := time.Now().Local().Hour()
			log.Info(fmt.Sprintf("currentTime:%d", currentHour))

			//From startTime to endTime
			if currentHour >= startTime && currentHour <= endTime {
				if scaler.Status.Status != apiv1alpha1.SCALED {
					log.Info("Starting to call scaleDeployment function")
					err := scaleDeployment(scaler, r, ctx, replicas)
					if err != nil {
						return ctrl.Result{}, err
					}
				}
			} else {
				if scaler.Status.Status == apiv1alpha1.SCALED {
					err := restoreDeployment(scaler, r, ctx)
					if err != nil {
						return ctrl.Result{}, err
					}
				}

			}

		} else {
			log.Info("Starting deletion flow.")
			if scaler.Status.Status == apiv1alpha1.SCALED {
				err := restoreDeployment(scaler, r, ctx)
				if err != nil {
					return ctrl.Result{}, err
				}
				log.Info("Remove finalizer")
				controllerutil.RemoveFinalizer(scaler, finalizer)
			}
			log.Info("Remove successfully")
		}

		//Add the number of replicas and namespace which managed by scaler to annotations
		if err := addAnnotations(scaler, r, ctx); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}

	startTime := scaler.Spec.Start
	endTime := scaler.Spec.End
	replicas := scaler.Spec.Replicas

	currentHour := time.Now().Local().Hour()
	log.Info(fmt.Sprintf("currentTime:%d", currentHour))

	//From startTime to endTime
	if currentHour >= startTime && currentHour <= endTime {
		if scaler.Status.Status != apiv1alpha1.SCALED {
			log.Info("Starting to call scaleDeployment function")
			err := scaleDeployment(scaler, r, ctx, replicas)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if scaler.Status.Status == apiv1alpha1.SCALED {
			err := restoreDeployment(scaler, r, ctx)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{RequeueAfter: time.Duration(10 * time.Second)}, nil
}

func restoreDeployment(scaler *apiv1alpha1.Scaler, r *ScalerReconciler, ctx context.Context) error {
	logger.Info("starting to return to the original state")
	for name, originalDeployInfo := range originalDeploymentInfo {
		deployment := &v1.Deployment{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: originalDeployInfo.Namespace,
		}, deployment); err != nil {
			return err
		}

		if deployment.Spec.Replicas != &originalDeployInfo.Replicas {
			deployment.Spec.Replicas = &originalDeployInfo.Replicas
			if err := r.Update(ctx, deployment); err != nil {
				return err
			}
		}
	}

	scaler.Status.Status = apiv1alpha1.RESTORED
	err := r.Status().Update(ctx, scaler)
	if err != nil {
		return err
	}

	return nil
}

func scaleDeployment(scaler *apiv1alpha1.Scaler, r *ScalerReconciler, ctx context.Context, replicas int32) error {
	//Iterate deployments from scaler Instance
	for _, deploy := range scaler.Spec.Deployments {
		//build up a new deployment instance
		deployment := &v1.Deployment{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      deploy.Name,
			Namespace: deploy.Namespace,
		}, deployment)
		if err != nil {
			return err
		}

		//Judging  the replica number of deployment equal to the scaler specified or not
		if deployment.Spec.Replicas != &replicas {
			deployment.Spec.Replicas = &replicas
			err := r.Update(ctx, deployment)
			if err != nil {
				scaler.Status.Status = apiv1alpha1.FAILURE
				r.Status().Update(ctx, scaler)
				return err
			}
			scaler.Status.Status = apiv1alpha1.SCALED
			r.Status().Update(ctx, scaler)
		}
	}
	return nil
}

func addAnnotations(scaler *apiv1alpha1.Scaler, r *ScalerReconciler, ctx context.Context) error {
	//Recording the origin number of replicas and the name of namespace
	for _, deploy := range scaler.Spec.Deployments {
		deployment := &v1.Deployment{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      deploy.Name,
			Namespace: deployment.Namespace,
		}, deployment); err != nil {
			return err
		}

		//Start recording
		if *deployment.Spec.Replicas != scaler.Spec.Replicas {
			logger.Info("add original state to originalDeploymentInfo map.")
			originalDeploymentInfo[deployment.Name] = apiv1alpha1.DeploymentInfo{
				*deployment.Spec.Replicas,
				deployment.Namespace,
			}
		}

	}

	//Add the original info to annotations
	for deploymentName, info := range originalDeploymentInfo {
		//Transfer info to json
		infoJson, err := json.Marshal(info)
		if err != nil {
			return err
		}

		//Saving infoJason to annotations map
		annotations[deploymentName] = string(infoJson)
	}

	//Updating the annotations of scaler
	scaler.ObjectMeta.Annotations = annotations
	err := r.Update(ctx, scaler)
	if err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ScalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1alpha1.Scaler{}).
		Named("scaler").
		Complete(r)
}
