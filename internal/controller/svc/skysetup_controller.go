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

/*

SkySetup.svc.skycluster.io composed of two objects:
- SkySetup.xrds.skycluster.io
- SkyGateway.xrds.skycluster.io

The SkySetup (xrds) object is not expected to experience any failure after creation.
This object creates the Keys, Security groups, etc.

The SkyGateway object is the main object that creates the
gateway VM. The gateway will be connected to the overlay server
and is the NAT for other VMs and services. The Gateway object,
however, can experience failures and should be able to recover
from failures.

We monitor the status of SkyGateway object once it is created
and upon discovering a number of failures, we will destroy the object
and create a new one, hoping that the new object will be healthy.

It is important to note that some settings of the SkyGateway
should be retained, such as private IP and perhaps the public IP.
We maintain theses settings in the Status field of main object.

*/

package svc

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	svcv1alpha1 "github.com/etesami/skycluster-manager/api/svc/v1alpha1"
	ctrlutils "github.com/etesami/skycluster-manager/internal/controller"
)

// SkySetupReconciler reconciles a SkySetup object
type SkySetupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skysetups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skysetups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skysetups/finalizers,verbs=update

func (r *SkySetupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	loggerName := "SkySetup"
	logger.Info(fmt.Sprintf("[%s]\t Reconciling SkySetup [%s]", loggerName, req.Name))

	// Fetch the SkySetup instance
	instance := &svcv1alpha1.SkySetup{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t unable to fetch SkySetup [%s]", loggerName, req.Name))
		// The object has been deleted and the its dependencies will be deleted
		// by the garbage collector
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Create the SkyGateway object
	stObj := &unstructured.Unstructured{}
	stObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "xrds.skycluster.io", Version: "v1alpha1", Kind: "SkySetup",
	})
	// TODO: Rename the object to instance.Name
	stObjName := "scinet-setup"
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: stObjName}, stObj); err != nil {
		// if the SkySetup object does not exist, we create it
		logger.Info(fmt.Sprintf("[%s]\t unable to fetch [SkySetup], creating a new one.", req.Name))
		stObj.SetName(stObjName)
		stObj.SetNamespace(instance.Namespace)
		stObj.SetLabels(instance.Labels)
		stObj.Object["spec"] = map[string]any{
			"forProvider": instance.Spec.ForProvider,
			"providerRef": instance.Spec.ProviderRef,
		}
		// set owner reference
		if err := ctrl.SetControllerReference(instance, stObj, r.Scheme); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to set owner reference for [SkySetup]", req.Name))
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, stObj); client.IgnoreAlreadyExists(err) != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to create [SkySetup]", req.Name))
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("[%s]\t [SkySetup] created, requeue the object.", req.Name))
		return ctrl.Result{Requeue: true}, nil
	}

	// Fetch the SkyGateway instance
	gwObj := &unstructured.Unstructured{}
	gwObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "xrds.skycluster.io", Version: "v1alpha1", Kind: "SkyGateway",
	})
	// TODO: Rename the object to instance.Name
	gwName := "scinet-gw"
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: gwName}, gwObj); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t unable to fetch [SkyGateway], creating a new one", req.Name))
		gwObj.SetName(gwName)
		gwObj.SetNamespace(instance.Namespace)
		gwObj.SetLabels(instance.Labels)
		gwObj.Object["spec"] = map[string]any{
			"forProvider": instance.Spec.ForProvider,
			"providerRef": instance.Spec.ProviderRef,
		}
		if err := ctrl.SetControllerReference(instance, gwObj, r.Scheme); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to set owner reference for [SkyGateway]", req.Name))
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, gwObj); client.IgnoreAlreadyExists(err) != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to create [SkyGateway]", req.Name))
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("[%s]\t [SkyGateway] created, requeue the object.", req.Name))
		return ctrl.Result{Requeue: true}, nil
	}

	// At these stage we have both objects and we can check their status
	// we update the status of the current object to whatever the status of the both objects are

	logger.Info(fmt.Sprintf("[%s]\t [SkySetup] and [SkyGateway] already exist, updating the status [%s]", loggerName, req.Name))
	stConditions, err1 := ctrlutils.GetNestedValue(stObj.Object, "status", "conditions")
	gwConditions, err2 := ctrlutils.GetNestedValue(gwObj.Object, "status", "conditions")
	if err1 != nil || err2 != nil {
		logger.Error(err1, fmt.Sprintf("[%s]\t unable to get conditions of SkySetup or SkyGateway", req.Name))
		return ctrl.Result{}, err1
	}
	// Check stConditions and gwConditions to be of type []any
	if _, ok := stConditions.([]any); !ok {
		if _, ok := gwConditions.([]any); !ok {
			logger.Info(fmt.Sprintf("[%s]\t SkySetup conditions is not of type []any", loggerName))
			return ctrl.Result{}, fmt.Errorf("SkySetup conditions is not of type []any")
		}
	}
	stConditionsList, gwConditionsList := stConditions.([]any), gwConditions.([]any)

	// Find the Ready condition in each of the objects
	stIdx := ctrlutils.IndexOfMapValue(stConditionsList, "type", "Ready")
	gwIdx := ctrlutils.IndexOfMapValue(gwConditionsList, "type", "Ready")
	// Get the Ready conditions of the SkySetup and SkyGateway
	stReadyCd := stConditionsList[stIdx].(map[string]any)
	gwReadyCd := gwConditionsList[gwIdx].(map[string]any)
	logger.Info(fmt.Sprintf("[%s]\t [SkySetup] Ready condition: %v", loggerName, stReadyCd["status"]))
	logger.Info(fmt.Sprintf("[%s]\t [SkyGateway] Ready condition: %v", loggerName, gwReadyCd["status"]))

	// TODO: We should only update the status if the status has changed
	objStReadyCd := ctrlutils.IndexOfConditionType(instance.Status.Conditions, "ReadySkySetup")
	objGwReadyCd := ctrlutils.IndexOfConditionType(instance.Status.Conditions, "ReadySkyGateway")
	shouldUpdate := false
	if objStReadyCd == -1 || objGwReadyCd == -1 {
		logger.Info(fmt.Sprintf("[%s]\t Ready condition does not exist, updating the status", loggerName))
		shouldUpdate = true
	} else if instance.Status.Conditions[objStReadyCd].Status != getStatus(stReadyCd["status"]) ||
		instance.Status.Conditions[objGwReadyCd].Status != getStatus(gwReadyCd["status"]) {
		shouldUpdate = true
	}
	if shouldUpdate {
		logger.Info(fmt.Sprintf("[%s]\t Status of the objects has changed, updating the status", loggerName))
		// If status exists in the list of conditions remove it first
		objCond := ctrlutils.RemoveConditionByType(instance.Status.Conditions, "ReadySkySetup")
		objCond = ctrlutils.RemoveConditionByType(objCond, "ReadySkyGateway")
		// Add the Ready condition of the SkySetup object
		t := metav1.Time{}
		if err := t.UnmarshalQueryParameter(stReadyCd["lastTransitionTime"].(string)); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to unmarshal lastTransitionTime", req.Name))
			return ctrl.Result{}, err
		} else {
			objCond = append(objCond, metav1.Condition{
				Type:               "ReadySkySetup",
				Status:             getStatus(stReadyCd["status"]),
				Message:            ctrlutils.SafeString(stReadyCd["message"]),
				Reason:             ctrlutils.SafeString(stReadyCd["reason"]),
				LastTransitionTime: metav1.Time{Time: t.Time},
			})
		}
		// Add the Ready condition of the SkyGateway object
		if err := t.UnmarshalQueryParameter(gwReadyCd["lastTransitionTime"].(string)); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to unmarshal lastTransitionTime", req.Name))
			return ctrl.Result{}, err
		} else {
			objCond = append(objCond, metav1.Condition{
				Type:               "ReadySkyGateway",
				Status:             getStatus(gwReadyCd["status"]),
				Message:            ctrlutils.SafeString(gwReadyCd["message"]),
				Reason:             ctrlutils.SafeString(gwReadyCd["reason"]),
				LastTransitionTime: metav1.Time{Time: t.Time},
			})
		}
		instance.Status.Conditions = objCond
		// TODO: Move update status block to the end
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to update status of SkySetup", req.Name))
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("[%s]\t Updated the conditions [ReadySkySetup] and [ReadySkyGateway].", loggerName))
	}

	// Check the status of SkySetup and SkyGateway objects
	if ctrlutils.SafeString(stReadyCd["status"]) != "True" || ctrlutils.SafeString(gwReadyCd["status"]) != "True" {
		logger.Info(fmt.Sprintf("[%s]\t SkySetup or SkyGateway is not ready, requeue the object in 30 seconds", loggerName))
		// TODO: adjust the requeue time
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// If the Ready status of the SkySetup and SkyGateway are True,
	// we can now check the health of the service using the monitoring data
	// For this purpose, we only support ssh connection and
	// we use provider-ssh to connect to the machine and query its health
	// A provider config data should be created as part of SkyGateway object
	// and the ProviderConfig data should be available through the status field
	// The providerConfig name is availalbe in status.gateway.providerConfig

	pConfigName, err := ctrlutils.GetNestedValue(gwObj.Object, "status", "gateway", "providerConfig")
	if err != nil {
		logger.Error(err, fmt.Sprintf("[%s]\t unable to get providerConfig name", req.Name))
		return ctrl.Result{}, err
	}
	if _, ok := pConfigName.(string); !ok {
		logger.Info(fmt.Sprintf("[%s]\t providerConfig name is not of type string", loggerName))
		return ctrl.Result{}, fmt.Errorf("providerConfig name is not of type string")
	}
	logger.Info(fmt.Sprintf("[%s]\t Script providerConfig name: [%s]", loggerName, pConfigName.(string)))
	// Now we can create Script object using unstructured object
	scObj := &unstructured.Unstructured{}
	scObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "xrds.skycluster.io", Version: "v1alpha1", Kind: "Script",
	})
	if err := r.Get(ctx, req.NamespacedName, scObj); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t unable to fetch [Script], creating one.", req.Name))
		scObj.SetName(instance.Name)
		scObj.SetNamespace(req.Namespace)
		scObj.SetLabels(instance.Labels)
		scObj.Object["spec"] = map[string]any{
			"forProvider": map[string]any{
				"statusCheckScript": instance.Spec.Monitoring.CheckCommand,
				"sudoEnabled":       true,
			},
			"providerConfigRef": map[string]any{
				"name": ctrlutils.SafeString(pConfigName),
			},
		}
		logger.Info(fmt.Sprintf("[%s]\t Setting owner [%s], [%s]", loggerName, scObj.GetAPIVersion(), scObj.GetKind()))
		// set owner reference
		if err := ctrl.SetControllerReference(instance, scObj, r.Scheme); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to set owner reference for [Script]", req.Name))
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("[%s]\t Creating [Script] object...", loggerName))
		if err := r.Create(ctx, scObj); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to create Script object", req.Name))
			return ctrl.Result{}, err
		}
		// TODO: adjust the time
		logger.Info(fmt.Sprintf("[%s]\t [Script] created, requeue the object in 15 seconds", loggerName))
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	logger.Info(fmt.Sprintf("[%s]\t [Script] object already exists, checking the status", loggerName))
	// The Script object exists, we can now check the status of the object
	// TODO: Initially the status code does not exist, so the code will fail here
	statusCode, err := ctrlutils.GetNestedValue(scObj.Object, "status", "atProvider", "statusCode")
	if err != nil {
		logger.Error(err, fmt.Sprintf("[%s]\t unable to get statusCode of [Script]", req.Name))
		return ctrl.Result{}, err
	}
	logger.Info(fmt.Sprintf("[%s]\t statusCode: [%d]", loggerName, statusCode))
	stdOut, err1 := ctrlutils.GetNestedValue(scObj.Object, "status", "atProvider", "stdout")
	stdErr, err2 := ctrlutils.GetNestedValue(scObj.Object, "status", "atProvider", "stderr")
	if err1 != nil || err2 != nil {
		logger.Error(err1, fmt.Sprintf("[%s]\t unable to get stdout or stderr of [Script]", req.Name))
		return ctrl.Result{}, err1
	}
	// Get the condition Ready of the current object
	objReadtCdIdx := ctrlutils.IndexOfConditionType(instance.Status.Conditions, "Ready")
	if objReadtCdIdx == -1 {
		// Condition does not exist, append a new Ready conditino
		instance.Status.Conditions = append(instance.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionUnknown,
			Reason:             "Unknown",
			Message:            "",
			LastTransitionTime: metav1.Time{Time: time.Now()},
		})
		objReadtCdIdx = ctrlutils.IndexOfConditionType(instance.Status.Conditions, "Ready")
	}
	objReadyCd := instance.Status.Conditions[objReadtCdIdx]
	// If the status code is 0, and we don't have the ready condition set to True
	// we proceed to update the status of the current object
	if statusCode.(int64) == 0 && objReadyCd.Status != "True" {
		logger.Info(fmt.Sprintf("[%s]\t Script is healthy", loggerName))
		// add a condition to the status of the current object
		objCond := ctrlutils.RemoveConditionByType(instance.Status.Conditions, "Ready")
		objCond = append(objCond, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Message:            ctrlutils.SafeString(stdOut) + ctrlutils.SafeString(stdErr),
			Reason:             "Healthy",
			LastTransitionTime: metav1.Time{Time: time.Now()},
		})
		logger.Info(fmt.Sprintf("[%s]\t Updating the status of SkySetup appending to conditions.", loggerName))
		instance.Status.Conditions = objCond
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to update status of SkySetup", req.Name))
			return ctrl.Result{}, err
		}
		// TODO: Do we need to delete the script?
		// logger.Info(fmt.Sprintf("[%s]\t Script is healthy, deleting the [Script].", loggerName))
		// Delete the script and return
		// if err := r.Delete(ctx, scObj); err != nil {
		// 	logger.Error(err, fmt.Sprintf("[%s]\t unable to delete Script object", req.Name))
		// 	return ctrl.Result{}, err
		// }
		// TODO: We can requeue for the next iteration but we return now for simplicity
		logger.Info(fmt.Sprintf("[%s]\t [Done].", loggerName))
		return ctrl.Result{}, nil
	}
	// Only if the status code is not 0, and the current status is True we update the status
	if statusCode.(int64) != 0 && objReadyCd.Status == "True" {
		// Failure happened
		// We pass the stdout and stderr to the status of the current object
		// Increase the counter and requeue the object to check the health again
		logger.Info(fmt.Sprintf("[%s]\t Script is not healthy StatusCode: [%d], Counter: [%d]", loggerName, statusCode.(int64), instance.Status.Retries))
		objCond := ctrlutils.RemoveConditionByType(instance.Status.Conditions, "Ready")
		objCond = append(objCond, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Message:            ctrlutils.SafeString(stdOut) + ctrlutils.SafeString(stdErr),
			Reason:             "Unhealthy",
			LastTransitionTime: metav1.Time{Time: time.Now()},
		})
		logger.Info(fmt.Sprintf("[%s]\t Updated the conditions.", loggerName))
		instance.Status.Conditions = objCond
		// we let the script to remain in the system, hoping that it will reconcile
		// we wait for "retries" times before we delete the object and create it again.
		instance.Status.Retries++
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to update status of SkySetup", req.Name))
			return ctrl.Result{}, err
		}
		if instance.Status.Retries > instance.Spec.Monitoring.Schedule.Retries {
			logger.Info(fmt.Sprintf("[%s]\t Maximum retries reached, deleting the [Script].", loggerName))
			// We have reached the maximum number of retries
			// we delete the object and create it again
			if instance.Spec.Monitoring.FailureAction == "RECREATE" {
				logger.Info(fmt.Sprintf("[%s]\t Recreating the [SkySetup] object...", loggerName))
				// if err := r.Delete(ctx, scObj); err != nil {
				// 	logger.Error(err, fmt.Sprintf("[%s]\t unable to delete Script object", req.Name))
				// 	return ctrl.Result{}, err
				// }
				// logger.Info(fmt.Sprintf("[%s]\t Script object deleted...", loggerName))
				// if err := r.Delete(ctx, gwObj); err != nil {
				// 	logger.Error(err, fmt.Sprintf("[%s]\t unable to delete SkyGateway object", req.Name))
				// 	return ctrl.Result{}, err
				// }
				// logger.Info(fmt.Sprintf("[%s]\t [SkyGateway] object deleted...", loggerName))
				// We requeue the object and try again
				// TODO: Adjust the time
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
		}
	}

	// we have created both SkySetup and SkyGateway objects or we already have them
	// now we check the status of SkyGateway and check its health
	// To check the status of SkyGateway, we need to consider two things:
	//   1. The conditions of the corresponding xrd object
	//   2. Using the monitoring data as part of "instance" object,
	//      we should establish a monitoring connection and check the service health
	//      For SkyGateway, the connnection should be established through ssh and the
	//      "checkCommand" script should be executed to determine the health of the serrvice.

	// To do this, we first query the conditions of xrds, and look for the Ready condition,
	// if this condition exists, we continue to the next step.
	// xrdStatus, err := GetNestedField(gwObj.Object, "status", "conditions")

	// TO check the serivce status, we need to establish a connection to the servive.

	// if both items are created we have to get the status of them
	// we can either watch the status of the objects created, or we can
	// requeue the current object and check the status of the objects in the next iteration
	// I think requeueing is better, simpler and more easy to implement
	logger.Info(fmt.Sprintf("[%s]\t Nothing to update, statusCode: [%d].", loggerName, statusCode.(int64)))
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkySetupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&svcv1alpha1.SkySetup{}).
		Named("svc-skysetup").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}
