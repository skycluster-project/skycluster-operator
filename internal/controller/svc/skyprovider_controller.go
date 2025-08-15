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

	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	sv1a1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
	ctrlutils "github.com/skycluster-project/skycluster-operator/internal/controller"
)

// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyproviders/finalizers,verbs=update

// SkyProviderReconciler reconciles a SkyProvider object
type SkyProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var (
	// SyncPeriodDefault is the default time period (5m) for requeueing the object
	SyncPeriodDefault = 2 * time.Minute
	// SyncPeriodHealthCheck is the default time period (10s) for requeueing the object
	// when the object has been created and we are checking the health of the service
	SyncPeriodHealthCheck = 10 * time.Second
	// SyncPeriodRequeue is the default time period (1s) for requeueing the object
	SyncPeriodRequeue = 1 * time.Second
)

func (r *SkyProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	loggerName := "SkyProvider"
	logger.Info(fmt.Sprintf("[%s]\t Reconciling SkySetup [%s]", loggerName, req.Name))

	// Fetch the SkySetup instance
	instance := &sv1a1.SkyProvider{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t unable to fetch SkySetup [%s]", loggerName, req.Name))
		// The object has been deleted and the its dependencies will be deleted
		// by the garbage collector, if there is nothing to check, we don't need to requeue
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// dep := map[string]map[string]string{
	// 	"skysetup": {"group": "xrds.skycluster.io", "version": "v1alpha1", "kind": "SkySetup"},
	// 	"skygw":    {"group": "xrds.skycluster.io", "version": "v1alpha1", "kind": "SkyGateway"},
	// }

	// // Create the SkyGateway object
	// stObj := &unstructured.Unstructured{}
	// stObj.SetGroupVersionKind(schema.GroupVersionKind{
	// 	Group: dep["skysetup"]["group"], Version: dep["skysetup"]["version"], Kind: dep["skysetup"]["kind"],
	// })
	// if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: instance.Name}, stObj); err != nil {
	// 	if err := r.createSkySetup(instance, stObj); err != nil {
	// 		logger.Error(err, fmt.Sprintf("[%s]\t unable to create SkySetup", loggerName))
	// 		return ctrl.Result{}, err
	// 	}
	// 	logger.Info(fmt.Sprintf("[%s]\t [SkySetup] created, requeue the object.", req.Name))
	// 	return ctrl.Result{RequeueAfter: SyncPeriodRequeue}, nil
	// }

	// // Fetch the SkyGateway instance
	// gwObj := &unstructured.Unstructured{}
	// gwObj.SetGroupVersionKind(schema.GroupVersionKind{
	// 	Group: dep["skygw"]["group"], Version: dep["skygw"]["version"], Kind: dep["skygw"]["kind"],
	// })
	// if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: instance.Name}, gwObj); err != nil {
	// 	if err := r.createSkyGateway(instance, gwObj); err != nil {
	// 		logger.Error(err, fmt.Sprintf("[%s]\t unable to create SkyGateway", loggerName))
	// 		return ctrl.Result{}, err
	// 	}
	// 	logger.Info(fmt.Sprintf("[%s]\t [SkyGateway] created, requeue the object.", req.Name))
	// 	return ctrl.Result{RequeueAfter: SyncPeriodRequeue}, nil
	// }

	// // At these stage we have both objects and we can check their status
	// // we update the status of the current object to whatever the status of the both objects are
	// logger.Info(fmt.Sprintf("[%s]\t [SkySetup] and [SkyGateway] already exist, updating the status [%s]", loggerName, req.Name))
	// objCds1, err1 := updateCondition(instance, stObj, "Ready", "ReadySkySetup")
	// if err1 != nil {
	// 	logger.Error(err1, fmt.Sprintf("[%s]\t unable to update Ready condition of SkySetup", req.Name))
	// 	return ctrl.Result{}, err1
	// }
	// instance.Status.Conditions = *objCds1
	// objCds2, err2 := updateCondition(instance, gwObj, "Ready", "ReadySkyGateway")
	// if err2 != nil {
	// 	logger.Error(err2, fmt.Sprintf("[%s]\t unable to update Ready condition of SkyGateway", req.Name))
	// 	return ctrl.Result{}, err2
	// }
	// instance.Status.Conditions = *objCds2
	// if err := r.Status().Update(ctx, instance); err != nil {
	// 	logger.Error(err, fmt.Sprintf("[%s]\t unable to update status of SkySetup", req.Name))
	// 	return ctrl.Result{}, err
	// }

	// // If both ReadySkySetup and ReadySkyGateway are ready, we proceed
	// cds := instance.Status.Conditions
	// condReadySkyS := ctrlutils.GetTypedConditionStatus(cds, "ReadySkySetup")
	// condReadySkyG := ctrlutils.GetTypedConditionStatus(cds, "ReadySkyGateway")
	// if *condReadySkyS != "True" || *condReadySkyG != "True" {
	// 	logger.Info(fmt.Sprintf("[%s]\t SkySetup or SkyGateway is not ready, requeue the object", loggerName))
	// 	return ctrl.Result{RequeueAfter: SyncPeriodHealthCheck}, nil
	// }

	// // If all dependency objects are okay,
	// // we can now check the health of the service using the monitoring data
	// // We only support ssh connection for now and
	// // we use provider-ssh to connect to the machine and query its health
	// // A provider config data should be created as part of SkyGateway object
	// // and the ProviderConfig data should be available through the status field
	// // Since the dependency services are ready, we expect providerConfigName to be available
	// scObj, err := r.getCreateScriptObject(instance, gwObj, req.NamespacedName)
	// if err != nil {
	// 	logger.Error(err, fmt.Sprintf("[%s]\t unable to create Script object", req.Name))
	// 	return ctrl.Result{}, err
	// }

	// // The Script object exists, we can now check the status of the object
	// // In addition to statusCode which shows the result of status-check code execution,
	// // we also need to check the special `ScriptExecuted` condition of the object.
	// // The provider-ssh provider sets this condition to True if the remote script
	// // is executed successfully and set the `statusCode` to the returned status code.
	// // We first check the `ScriptExecuted` condition and we increase the retries counter
	// // if the reason for this condition falls within the given set.
	// // If the condition is set to True we reset retries counter, and then
	// // and we check the statusCode.

	// logger.Info(fmt.Sprintf("[%s]\t [Script] object already exists, checking the status", loggerName))
	// found, scReadyCd, err := ctrlutils.GetUnstructuredConditionByType(scObj, "ScriptExecuted")
	// if err != nil {
	// 	logger.Info(fmt.Sprintf("[%s]\t unable to get (ScriptExecuted) condition of [Script]", loggerName))
	// 	return ctrl.Result{}, err
	// }
	// if !found {
	// 	// If ready condition is not found, something is wronge with the object and
	// 	// I don't expect it to be ready without human intervention, so we just return
	// 	return ctrl.Result{}, fmt.Errorf("condition does not exist for [Script]")
	// }

	// // Now if the Ready condition of Script exists, we should have it set to True, otherwise
	// // the service may not be reachable or somehting is wrong with the object
	// // and as a result we cannot connect and execute the script
	// if scReadyCd["status"] != "True" {
	// 	// We should further check the Reason of the condition, some errors that causes "Ready" condition
	// 	// to be false, should not be considered as a reason for requeuing the object
	// 	reasons := svcv1alpha1.ReasonsForServices
	// 	if ctrlutils.StringInSlice(ctrlutils.SafeString(scReadyCd["reason"]), reasons) {
	// 		logger.Info(fmt.Sprintf("[%s]\t [Script] is not ready, Retries: [%d]/[%d]", loggerName, instance.Status.Retries, instance.Spec.Monitoring.Schedule.Retries))
	// 		setConditionNotReady(instance, ctrlutils.SafeString(scReadyCd["reason"]), "Script is not ready")
	// 		// When the underlay resource is not available, we can delete the lowest level composite object
	// 		// and should avoid getting involved with the provider-specific resources (i.e. managed  resources).
	// 		err := r.incrementRetriesAndDelete(instance, scObj, gwObj)
	// 		if err != nil {
	// 			return ctrl.Result{}, fmt.Errorf("unable to increment retries and delete the objects: %v", err)
	// 		}
	// 	}
	// 	return ctrl.Result{RequeueAfter: SyncPeriodHealthCheck}, nil
	// }

	// // Now that Ready condition is set to True, we can check the status code
	// // We reset retries counter and let the script execution determines the status of the service
	// instance.Status.Retries = 0
	// // Initially the status code does not exist, we have to wait for the script to be executed
	// // Consequently the code will fail here
	// statusCode, stdOut, stdErr, err := getScriptData(scObj)
	// if err != nil {
	// 	logger.Info(fmt.Sprintf("[%s]\t unable to get status code of [Script]: %v", loggerName, err))
	// 	return ctrl.Result{}, err
	// }
	// shouldUpdate := determineShouldUpdate(instance, statusCode)
	// if shouldUpdate {
	// 	logger.Info(fmt.Sprintf("[%s]\t [Script] Updating the status of SkyProvider", loggerName))
	// 	message := stdOut + stdErr
	// 	if statusCode != 0 {
	// 		setConditionNotReady(instance, "Unhealthy", message)
	// 		// we let the script to remain in the system, hoping that it will reconcile
	// 		// we wait for "retries" times before we delete the object and create it again.
	// 		// If we reach the maximum number of retries we delete the SkyGateway object a new object
	// 		// will be created in the next iteration.
	// 		// We should be very careful about this step, and only proceed if the dependency
	// 		// objects are reported to be Ready. Type of issues that may not reported by
	// 		// the objects itself, like memory failure is handled here. Deleting a SkyGateway
	// 		// for example is detected at the Manage Resource level and the object is
	// 		// recreated by provider's controller. So a careful composition design is required
	// 		logger.Info(fmt.Sprintf("[%s]\t Retries: [%d]/[%d]", loggerName, instance.Status.Retries, instance.Spec.Monitoring.Schedule.Retries))
	// 		if err := r.incrementRetriesAndDelete(instance, scObj, gwObj); err != nil {
	// 			// Immediately requeuing the object will cause a new object to be created
	// 			// and often a conflict will occur between the object that is being deleted
	// 			// and the new object that is being created. We should wait for a while
	// 			return ctrl.Result{RequeueAfter: SyncPeriodHealthCheck}, err
	// 		}
	// 	}
	// 	// The service is healthy, update the status of the object and reset the `retries`
	// 	if statusCode == 0 {
	// 		setConditionReady(instance, "Healthy", message)
	// 		instance.Status.Retries = 0
	// 		logger.Info(fmt.Sprintf("[%s]\t [Script] Script is updated and healthy", loggerName))
	// 	}
	// 	if err := r.Status().Update(ctx, instance); err != nil {
	// 		logger.Error(err, fmt.Sprintf("[%s]\t unable to update status of SkySetup", req.Name))
	// 		return ctrl.Result{}, err
	// 	}
	// }

	// logger.Info(fmt.Sprintf("[%s]\t Object is updated. StatusCode: [%d].", loggerName, statusCode))
	return ctrl.Result{RequeueAfter: SyncPeriodDefault}, nil
}

// getScriptData gets the status code, stdout, and stderr of the Script object
func getScriptData(obj *unstructured.Unstructured) (int64, string, string, error) {
	statusCode, err := ctrlutils.GetNestedValue(obj.Object, "status", "atProvider", "statusCode")
	if err != nil {
		return -1, "", "", fmt.Errorf("unable to get statusCode of [Script]: %v", err)
	}
	statusCodeInt, ok := statusCode.(int64)
	if !ok {
		return -1, "", "", fmt.Errorf("statusCode is not int64: %v", statusCode)
	}
	stdOut, err := ctrlutils.GetNestedValue(obj.Object, "status", "atProvider", "stdout")
	if err != nil {
		return -1, "", "", fmt.Errorf("unable to get stdout of [Script]: %v", err)
	}
	stdErr, err := ctrlutils.GetNestedValue(obj.Object, "status", "atProvider", "stderr")
	if err != nil {
		return -1, "", "", fmt.Errorf("unable to get stderr of [Script]: %v", err)
	}
	return statusCodeInt, ctrlutils.SafeString(stdOut), ctrlutils.SafeString(stdErr), nil
}

func setConditionReady(instance *sv1a1.SkyProvider, conditionReason, conditionMessage string) {
	instance.Status.Conditions = ctrlutils.SetTypedCondition(instance.Status.Conditions, "Ready", metav1.ConditionTrue, conditionReason, conditionMessage, metav1.Now())
}

func setConditionNotReady(instance *sv1a1.SkyProvider, conditionReason, conditionMessage string) {
	instance.Status.Conditions = ctrlutils.SetTypedCondition(instance.Status.Conditions, "Ready", metav1.ConditionFalse, conditionReason, conditionMessage, metav1.Now())
}

// updateCondition updates the condition `conditionTargetName` of the object
// based on the condition `conditionName` of the given object `obj`
func updateCondition(instance *sv1a1.SkyProvider, obj *unstructured.Unstructured, conditionName, conditionTargetName string) (*[]metav1.Condition, error) {
	f1, objCd, err := ctrlutils.GetUnstructuredConditionByType(obj, conditionName)
	if err != nil {
		return nil, fmt.Errorf("unable to get Ready condition: %v", err)
	}
	if !f1 {
		instance.Status.Conditions = ctrlutils.SetTypedCondition(
			instance.Status.Conditions, conditionTargetName,
			metav1.ConditionFalse,
			hv1a1.ReasonServiceNotReady,
			"services is not ready",
			metav1.Now())
	} else {
		instance.Status.Conditions = ctrlutils.SetTypedCondition(
			instance.Status.Conditions, conditionTargetName,
			ctrlutils.ParseConditionStatus(objCd["status"]),
			ctrlutils.SafeString(objCd["reason"]),
			ctrlutils.SafeString(objCd["message"]),
			metav1.Now())
	}
	return &instance.Status.Conditions, nil
}

func determineShouldUpdate(instance *sv1a1.SkyProvider, statusCode int64) bool {
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

// incrementRetriesAndDelete increments the retries counter and delete the list of objects
// if the retries counter is greater than the maximum number of retries
func (r *SkyProviderReconciler) incrementRetriesAndDelete(instance *sv1a1.SkyProvider, objs ...*unstructured.Unstructured) error {
	instance.Status.Retries++
	if instance.Status.Retries > instance.Spec.Monitoring.Schedule.Retries {
		if strings.ToLower(instance.Spec.Monitoring.FailureAction) == "recreate" {
			// if any data should be reused, we should handle it here
			// Delete the list of objects
			// TODO: Uncomment the following code
			// for _, obj := range objs {
			// 	if err := r.Delete(context.Background(), obj); err != nil {
			// 		return err
			// 	}
			// }
		}
		// Reset the retries counter
		instance.Status.Retries = 0
	}
	if err := r.Status().Update(context.Background(), instance); err != nil {
		return err
	}
	return nil
}

func (r *SkyProviderReconciler) getCreateScriptObject(instance *sv1a1.SkyProvider, obj *unstructured.Unstructured, reqNsN types.NamespacedName) (*unstructured.Unstructured, error) {
	pConfigName, err := getProviderCfgName(obj)
	if err != nil {
		return nil, fmt.Errorf("unable to get providerConfig name: %v", err)
	}
	// Now we can create Script object using unstructured object
	scObj := &unstructured.Unstructured{}
	scObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "xrds.skycluster.io", Version: "v1alpha1", Kind: "Script",
	})
	if err := r.Get(context.Background(), reqNsN, scObj); err != nil {
		scObj.SetName(instance.Name)
		scObj.SetNamespace(instance.Namespace)
		scObj.SetLabels(ctrlutils.MergeStringMaps(instance.Labels, addDefaultLabels(instance.Spec.ProviderRef)))
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
		if err := r.setOwnerandCreate(scObj, instance); err != nil {
			return nil, fmt.Errorf("unable to create Script object: %v", err)
		}
		return scObj, nil
	}
	// if the pConfigName is not as same as the one set in scObj,
	// we should update it
	if u, err := ctrlutils.UpdateNestedValue(scObj.Object, pConfigName, "spec", "providerConfigRef", "name"); err != nil {
		return nil, fmt.Errorf("unable to update providerConfigRef: %v", err)
	} else if u {
		if err := r.Update(context.Background(), scObj); err != nil {
			return nil, fmt.Errorf("unable to update Script object: %v", err)
		}
	}
	return scObj, nil
}

// setOwnerandCreate sets the owner reference and creates the object
func (r *SkyProviderReconciler) setOwnerandCreate(obj *unstructured.Unstructured, instance *sv1a1.SkyProvider) error {
	// set owner reference
	if err := ctrl.SetControllerReference(instance, obj, r.Scheme); err != nil {
		return fmt.Errorf("unable to set owner reference for object: %v", err)
	}
	if err := r.Create(context.Background(), obj); client.IgnoreAlreadyExists(err) != nil {
		return fmt.Errorf("unable to create object: %v", err)
	}
	return nil
}

// createSkySetup creates the SkySetup object
func (r *SkyProviderReconciler) createSkySetup(instance *sv1a1.SkyProvider, obj *unstructured.Unstructured) error {
	obj.SetName(instance.Name)
	obj.SetNamespace(instance.Namespace)
	obj.SetLabels(ctrlutils.MergeStringMaps(instance.Labels, addDefaultLabels(instance.Spec.ProviderRef)))
	obj.Object["spec"] = map[string]any{
		"forProvider": map[string]string{
			"vpcCidr": instance.Spec.ProviderGateway.VpcCidr,
		},
		"providerRef": instance.Spec.ProviderRef,
	}
	if err := r.setOwnerandCreate(obj, instance); err != nil {
		return err
	}
	return nil
}

func (r *SkyProviderReconciler) createSkyGateway(instance *sv1a1.SkyProvider, obj *unstructured.Unstructured) error {
	obj.SetName(instance.Name)
	obj.SetNamespace(instance.Namespace)
	obj.SetLabels(ctrlutils.MergeStringMaps(instance.Labels, addDefaultLabels(instance.Spec.ProviderRef)))
	obj.Object["spec"] = generateProviderGwSpec(instance.Spec)
	if err := r.setOwnerandCreate(obj, instance); err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sv1a1.SkyProvider{}).
		Named("svc-skyprovider").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}
