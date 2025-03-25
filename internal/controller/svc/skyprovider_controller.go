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
	"errors"
	"fmt"
	"strings"
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

// SkyProviderReconciler reconciles a SkyProvider object
type SkyProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var (
	// SyncPeriodDefault is the default time period (5m) for requeueing the object
	SyncPeriodDefault = 5 * time.Minute
	// SyncPeriodHealthCheck is the default time period (10s) for requeueing the object
	// when the object has been created and we are checking the health of the service
	SyncPeriodHealthCheck = 10 * time.Second
	// SyncPeriodRequeue is the default time period (1s) for requeueing the object
	SyncPeriodRequeue = 1 * time.Second
)

// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyproviders/finalizers,verbs=update

func (r *SkyProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	loggerName := "SkyProvider"
	logger.Info(fmt.Sprintf("[%s]\t Reconciling SkySetup [%s]", loggerName, req.Name))

	// Fetch the SkySetup instance
	instance := &svcv1alpha1.SkyProvider{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t unable to fetch SkySetup [%s]", loggerName, req.Name))
		// The object has been deleted and the its dependencies will be deleted
		// by the garbage collector, if there is nothing to check, we don't need to requeue
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Create the SkyGateway object
	stObj := &unstructured.Unstructured{}
	stObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "xrds.skycluster.io", Version: "v1alpha1", Kind: "SkySetup",
	})
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: instance.Name}, stObj); err != nil {
		// if the SkySetup object does not exist, we create it
		logger.Info(fmt.Sprintf("[%s]\t unable to fetch [SkySetup], creating a new one.", req.Name))
		stObj.SetName(instance.Name)
		stObj.SetNamespace(instance.Namespace)
		stObj.SetLabels(ctrlutils.MergeStringMaps(instance.Labels, addDefaultLabels(instance.Spec.ProviderRef)))
		stObj.Object["spec"] = map[string]any{
			"forProvider": map[string]string{
				"vpcCidr": instance.Spec.ProviderGateway.VpcCidr,
			},
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
		return ctrl.Result{RequeueAfter: SyncPeriodRequeue}, nil
	}

	// Fetch the SkyGateway instance
	gwObj := &unstructured.Unstructured{}
	gwObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "xrds.skycluster.io", Version: "v1alpha1", Kind: "SkyGateway",
	})
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: instance.Name}, gwObj); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t unable to fetch [SkyGateway], creating a new one", req.Name))
		gwObj.SetName(instance.Name)
		gwObj.SetNamespace(instance.Namespace)
		gwObj.SetLabels(instance.Labels)
		gwObj.Object["spec"] = generateProviderGwSpec(instance.Spec)
		if err := ctrl.SetControllerReference(instance, gwObj, r.Scheme); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to set owner reference for [SkyGateway]", req.Name))
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, gwObj); client.IgnoreAlreadyExists(err) != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to create [SkyGateway]", req.Name))
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("[%s]\t [SkyGateway] created, requeue the object.", req.Name))
		return ctrl.Result{RequeueAfter: SyncPeriodRequeue}, nil
	}

	// At these stage we have both objects and we can check their status
	// we update the status of the current object to whatever the status of the both objects are
	// This step is not necessary, but it is good to provider the user with the status of the dependency objects
	logger.Info(fmt.Sprintf("[%s]\t [SkySetup] and [SkyGateway] already exist, updating the status [%s]", loggerName, req.Name))
	f1, stReadyCd, err1 := ctrlutils.GetUnstructuredConditionByType(stObj, "Ready")
	f2, gwReadyCd, err2 := ctrlutils.GetUnstructuredConditionByType(gwObj, "Ready")
	if err1 != nil || err2 != nil {
		logger.Error(errors.Join(err1, err2), fmt.Sprintf("[%s]\t unable to get Ready condition", req.Name))
		// There is a problem with either of these objects, we requeue the object hoping
		// their status will be updated in the next iteration
		return ctrl.Result{RequeueAfter: SyncPeriodHealthCheck}, errors.Join(err1, err2)
	}
	if !f1 {
		logger.Info(fmt.Sprintf("[%s]\t Ready condition does not exist for SkySetup", loggerName))
		instance.Status.Conditions = ctrlutils.SetTypedCondition(instance.Status.Conditions, "ReadySkySetup", metav1.ConditionUnknown, "Unknown", "SkySetup services is not ready", metav1.Now())
	}
	if !f2 {
		logger.Info(fmt.Sprintf("[%s]\t Ready condition does not exist for SkyGateway", loggerName))
		instance.Status.Conditions = ctrlutils.SetTypedCondition(instance.Status.Conditions, "ReadySkyGateway", metav1.ConditionUnknown, "Unknown", "SkyGateway services is not ready", metav1.Now())
	}
	if !f1 || !f2 {
		// set the condition of the current object to Unknown and requeue
		instance.Status.Conditions = ctrlutils.SetTypedCondition(instance.Status.Conditions, "Ready", metav1.ConditionUnknown, "Unknown", "Dependent services are not ready", metav1.Now())
		return ctrl.Result{RequeueAfter: SyncPeriodHealthCheck}, nil
	}

	// We should only update the status if the status has changed
	f1, objStReadyCd := ctrlutils.GetTypedCondition(instance.Status.Conditions, "ReadySkySetup")
	f2, objGwReadyCd := ctrlutils.GetTypedCondition(instance.Status.Conditions, "ReadySkyGateway")
	shouldUpdate := false
	if !f1 || !f2 {
		logger.Info(fmt.Sprintf("[%s]\t Ready condition does not exist, updating the status", loggerName))
		shouldUpdate = true
	}
	// If both conditions exist, we check if the status has changed
	if f1 && f2 {
		if objStReadyCd.Status != ctrlutils.ParseConditionStatus(stReadyCd["status"]) ||
			objGwReadyCd.Status != ctrlutils.ParseConditionStatus(gwReadyCd["status"]) {
			shouldUpdate = true
		}
	}
	if shouldUpdate {
		logger.Info(fmt.Sprintf("[%s]\t Status of the objects has changed, updating the status", loggerName))
		// Add the Ready condition of the SkySetup object
		t := metav1.Time{}
		if err := t.UnmarshalQueryParameter(stReadyCd["lastTransitionTime"].(string)); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to unmarshal lastTransitionTime", req.Name))
			return ctrl.Result{}, err
		} else {
			status := ctrlutils.ParseConditionStatus(stReadyCd["status"])
			message := ctrlutils.SafeString(stReadyCd["message"])
			reason := ctrlutils.SafeString(stReadyCd["reason"])
			instance.Status.Conditions = ctrlutils.SetTypedCondition(instance.Status.Conditions, "ReadySkySetup", status, reason, message, t)
		}
		// Add the Ready condition of the SkyGateway object
		if err := t.UnmarshalQueryParameter(gwReadyCd["lastTransitionTime"].(string)); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to unmarshal lastTransitionTime", req.Name))
			return ctrl.Result{}, err
		} else {
			status := ctrlutils.ParseConditionStatus(gwReadyCd["status"])
			message := ctrlutils.SafeString(gwReadyCd["message"])
			reason := ctrlutils.SafeString(gwReadyCd["reason"])
			instance.Status.Conditions = ctrlutils.SetTypedCondition(instance.Status.Conditions, "ReadySkyGateway", status, reason, message, t)
		}
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to update status of SkySetup", req.Name))
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("[%s]\t Updated the conditions [ReadySkySetup] and [ReadySkyGateway].", loggerName))
	}

	// Check the status of SkySetup and SkyGateway objects
	// If resources are not ready, we requeue the object
	if ctrlutils.SafeString(stReadyCd["status"]) != "True" || ctrlutils.SafeString(gwReadyCd["status"]) != "True" {
		logger.Info(fmt.Sprintf("[%s]\t SkySetup or SkyGateway is not ready, requeue the object", loggerName))
		return ctrl.Result{RequeueAfter: SyncPeriodHealthCheck}, nil
	}

	// If all dependency objects are okay,
	// we can now check the health of the service using the monitoring data
	// We only support ssh connection for now and
	// we use provider-ssh to connect to the machine and query its health
	// A provider config data should be created as part of SkyGateway object
	// and the ProviderConfig data should be available through the status field
	// Since the dependency services are ready, we expect providerConfigName to be available
	pConfigName, err := getProviderCfgName(gwObj)
	if err != nil {
		logger.Error(err, fmt.Sprintf("[%s]\t unable to get providerConfig name", req.Name))
		return ctrl.Result{}, err
	}
	logger.Info(fmt.Sprintf("[%s]\t [Script] Script providerConfig name: [%s]", loggerName, pConfigName))
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
				"name": pConfigName,
			},
		}
		// set owner reference
		if err := ctrl.SetControllerReference(instance, scObj, r.Scheme); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to set owner reference for [Script]", req.Name))
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, scObj); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t unable to create Script object", req.Name))
			return ctrl.Result{}, err
		}
		// TODO: adjust the time
		logger.Info(fmt.Sprintf("[%s]\t [Script] created, requeue the object in 15 seconds", loggerName))
		return ctrl.Result{RequeueAfter: SyncPeriodHealthCheck}, nil
	}

	// The Script object exists, we can now check the status of the object
	// In addition to statusCode which shows the result of status-check code execution,
	// we also need to check the Sync and Ready conditions of the object.
	// The provider-ssh provider set both the conditions to False if the object
	// is not ready, not accessible, or the script has not been executed for some reason
	// We first check the Ready condition, we proceed to increase the retries counter
	// if the script is not Ready. If the script is Ready, we check the statusCode
	// and proceed with the retries counter incrementation.

	logger.Info(fmt.Sprintf("[%s]\t [Script] object already exists, checking the status", loggerName))
	found, readyCd, err := ctrlutils.GetUnstructuredConditionByType(scObj, "Ready")
	if err != nil {
		logger.Info(fmt.Sprintf("[%s]\t unable to get Ready condition of [Script]", loggerName))
		return ctrl.Result{}, err
	}
	if !found {
		// If ready condition is not found, something is wronge with the object and
		// I don't expect it to be ready without human intervention, so we just return
		err := fmt.Errorf("[%s]\t Ready condition does not exist for [Script]", loggerName)
		return ctrl.Result{}, err
	}

	// Now if the Ready condition exists, we should have it set to True, otherwise
	// the service may not be reachable or somehting is wrong with the object
	// and as a result we cannot connect and execute the script
	if readyCd["status"] != "True" {
		logger.Info(fmt.Sprintf("[%s]\t [Script] is not ready, requeue the object", loggerName))
		err := r.incrementRetriesAndDelete(instance, scObj, gwObj)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("[%s]\t unable to increment retries and delete the objects: %v", loggerName, err)
		}
		return ctrl.Result{RequeueAfter: SyncPeriodHealthCheck}, nil
	}

	// Now that Ready condition is set to True, we can check the status code
	// We reset retries counter and let the script execution determines the status of the service
	instance.Status.Retries = 0
	// Initially the status code does not exist, we have to wait for the script to be executed
	// Consequently the code will fail here
	statusCode, err := ctrlutils.GetNestedValue(scObj.Object, "status", "atProvider", "statusCode")
	if err != nil {
		logger.Info(fmt.Sprintf("[%s]\t unable to get statusCode of [Script]", req.Name))
		// We have to return nil as error since returning error will requeue the object
		// with exponential backoff.
		return ctrl.Result{RequeueAfter: SyncPeriodHealthCheck}, nil
	}
	logger.Info(fmt.Sprintf("[%s]\t [Script] statusCode: [%d]", loggerName, statusCode))

	stdOut, err1 := ctrlutils.GetNestedValue(scObj.Object, "status", "atProvider", "stdout")
	stdErr, err2 := ctrlutils.GetNestedValue(scObj.Object, "status", "atProvider", "stderr")
	if err1 != nil || err2 != nil {
		logger.Error(errors.Join(err1, err2), fmt.Sprintf("[%s]\t unable to get stdout or stderr of [Script]", req.Name))
		return ctrl.Result{}, errors.Join(err1, err2)
	}

	shouldUpdate = r.determineShouldUpdate(instance, statusCode.(int64))
	if shouldUpdate {
		logger.Info(fmt.Sprintf("[%s]\t [Script] Updating the status of SkyProvider", loggerName))
		message := ctrlutils.SafeString(stdOut) + ctrlutils.SafeString(stdErr)
		if statusCode.(int64) != 0 {
			logger.Info(fmt.Sprintf("[%s]\t [Script] Script is not healthy. (StatusCode: [%d], Counter: [%d])", loggerName, statusCode.(int64), instance.Status.Retries))
			instance.Status.Conditions = ctrlutils.SetTypedCondition(instance.Status.Conditions, "Ready", metav1.ConditionFalse, "Unhealthy", message, metav1.Now())

			// we let the script to remain in the system, hoping that it will reconcile
			// we wait for "retries" times before we delete the object and create it again.

			// If we reach the maximum number of retries we delete the SkyGateway object
			// A new object will be created in the next iteration
			// We should be very careful about this step, and only proceed if the dependency
			// objects are reported to be Ready. Type of issues that may not reported by
			// the objects itself, like memory failure is handled here. Deleting a SkyGateway
			// for example is detected at the Manage Resource level and the object is
			// recreated by provider's controller. So a careful composition design is required
			if err := r.incrementRetriesAndDelete(instance, scObj, gwObj); err != nil {
				return ctrl.Result{}, err
			}
		}
		// The service is healthy, update the status of the object and reset the `retries`
		if statusCode.(int64) == 0 {
			instance.Status.Conditions = ctrlutils.SetTypedCondition(instance.Status.Conditions, "Ready", metav1.ConditionTrue, "Healthy", message, metav1.Now())
			instance.Status.Retries = 0
			if err := r.Status().Update(ctx, instance); err != nil {
				logger.Error(err, fmt.Sprintf("[%s]\t unable to update status of SkySetup", req.Name))
				return ctrl.Result{}, err
			}
			logger.Info(fmt.Sprintf("[%s]\t [Script] Script is updated and healthy", loggerName))
			return ctrl.Result{RequeueAfter: SyncPeriodDefault}, nil
		}

	}

	logger.Info(fmt.Sprintf("[%s]\t Object is updated. StatusCode: [%d].", loggerName, statusCode.(int64)))
	return ctrl.Result{RequeueAfter: SyncPeriodDefault}, nil
}

// incrementRetriesAndDelete increments the retries counter and delete the list of objects
// if the retries counter is greater than the maximum number of retries
func (r *SkyProviderReconciler) incrementRetriesAndDelete(instance *svcv1alpha1.SkyProvider, objs ...*unstructured.Unstructured) error {
	instance.Status.Retries++
	if err := r.Status().Update(context.Background(), instance); err != nil {
		return err
	}
	if instance.Status.Retries > instance.Spec.Monitoring.Schedule.Retries {
		if strings.ToLower(instance.Spec.Monitoring.FailureAction) == "recreate" {
			// TODO: if any data should be reused, we should handle it here
			// Delete the list of objects
			for _, obj := range objs {
				if err := r.Delete(context.Background(), obj); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *SkyProviderReconciler) determineShouldUpdate(instance *svcv1alpha1.SkyProvider, statusCode int64) bool {
	f, objReadyCd := ctrlutils.GetTypedCondition(instance.Status.Conditions, "Ready")
	// If  we don't have the ready condition set to True
	// or not updated according to the status code
	// we proceed to update the status of the current object
	if !f {
		return true
	} else {
		if statusCode == 0 && objReadyCd.Status != "True" {
			return true
		}
		if statusCode != 0 && objReadyCd.Status == "True" {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&svcv1alpha1.SkyProvider{}).
		Named("svc-skyprovider").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}
