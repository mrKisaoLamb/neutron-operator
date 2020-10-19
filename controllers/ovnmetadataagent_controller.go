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
	"fmt"
	"github.com/go-logr/logr"
	util "github.com/openstack-k8s-operators/lib-common/pkg/util"
	"github.com/openstack-k8s-operators/neutron-operator/pkg/common"
	"github.com/openstack-k8s-operators/neutron-operator/pkg/ovnmetadataagent"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"time"

	neutronv1beta1 "github.com/openstack-k8s-operators/neutron-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

const (
	OVNConfigMAP string = "common-config"
)
// OVNMetadataAgentReconciler reconciles a OVNMetadataAgent object
type OVNMetadataAgentReconciler struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=neutron.openstack.org,resources=ovnmetadataagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neutron.openstack.org,resources=ovnmetadataagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;create;update;delete;
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;create;update;delete;
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;delete;
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;create;update;delete;
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;create;update;delete;

func (r *OVNMetadataAgentReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("ovnmetadata", req.NamespacedName)
	r.Log.Info("Reconciling OVNController")

	// Fetch the OVNController instance
	instance := &neutronv1beta1.OVNMetadataAgent{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, instance)
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
	// get instance.Spec.CommonConfigMap which holds general information on the OSP environment
	// TODO: handle commonConfigMap data change
	commonConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: instance.Spec.CommonConfigMap, Namespace: instance.Namespace}, commonConfigMap)
	if err != nil && errors.IsNotFound(err) {
		r.Log.Error(err, instance.Spec.CommonConfigMap+" ConfigMap not found!", "Instance.Namespace", instance.Namespace, "Instance.Name", instance.Name)
		return ctrl.Result{}, err
	}
	if err := controllerutil.SetControllerReference(instance, commonConfigMap, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	ovnConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name:OVNConfigMAP, Namespace: instance.Namespace}, ovnConfigMap)
	if err != nil && errors.IsNotFound(err) {
		r.Log.Error(err, OVNConfigMAP+" ConfigMap not found!", "Instance.Namespace", instance.Namespace, "Instance.Name", instance.Name)
		return ctrl.Result{}, err
	}
	if err := controllerutil.SetControllerReference(instance, ovnConfigMap, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	// TemplatesConfigMap
	templatesConfigMap := ovnmetadataagent.TemplatesConfigMap(instance, commonConfigMap, ovnConfigMap, instance.Name+"-templates")
	if err := controllerutil.SetControllerReference(instance, templatesConfigMap, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	// Check if this TemplatesConfigMap already exists
	foundTemplatesConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: templatesConfigMap.Name, Namespace: templatesConfigMap.Namespace}, foundTemplatesConfigMap)
	if err != nil && errors.IsNotFound(err) {
		r.Log.Info("Creating a new TemplatesConfigMap", "TemplatesConfigMap.Namespace", templatesConfigMap.Namespace, "Job.Name", templatesConfigMap.Name)
		err = r.Client.Create(context.TODO(), templatesConfigMap)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if !reflect.DeepEqual(templatesConfigMap.Data, foundTemplatesConfigMap.Data) {
		r.Log.Info("Updating TemplatesConfigMap")
		templatesConfigMap.Data = foundTemplatesConfigMap.Data
	}

	templatesConfigMapHash, err := util.ObjectHash(templatesConfigMap.Data)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error calculating configuration hash: %v", err)
	}
	r.Log.Info("TemplatesConfigMapHash: ", "Data Hash:", templatesConfigMapHash)

	// Define a new Daemonset object
	ds := newDaemonsetOVNMetadataAgent(instance, instance.Name, templatesConfigMapHash)
	dsHash, err := util.ObjectHash(ds)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error calculating configuration hash: %v", err)
	}
	r.Log.Info("DaemonsetHash: ", "Daemonset Hash:", dsHash)

	// Set OVNController instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, ds, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Check if this Daemonset already exists
	found := &appsv1.DaemonSet{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		r.Log.Info("Creating a new Daemonset", "Ds.Namespace", ds.Namespace, "Ds.Name", ds.Name)
		err = r.Client.Create(context.TODO(), ds)
		if err != nil {
			return ctrl.Result{}, err
		}

		// Daemonset created successfully - don't requeue
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	} else {
		if instance.Status.DaemonsetHash != dsHash {
			r.Log.Info("Daemonset Updated")
			found.Spec = ds.Spec
			err = r.Client.Update(context.TODO(), found)
			if err != nil {
				return ctrl.Result{}, err
			}
			r.setDaemonsetHash(instance, dsHash)
			return ctrl.Result{RequeueAfter: time.Second}, err
		}
	}

	// Daemonset already exists - don't requeue
	r.Log.Info("Skip reconcile: Daemonset already exists", "Ds.Namespace", found.Namespace, "Ds.Name", found.Name)
	return ctrl.Result{}, nil
}

func (r *OVNMetadataAgentReconciler) setDaemonsetHash(instance *neutronv1beta1.OVNMetadataAgent, hashStr string) error {
	if hashStr != instance.Status.DaemonsetHash {
		instance.Status.DaemonsetHash = hashStr
		if err := r.Client.Status().Update(context.TODO(), instance); err != nil {
			return err
		}
	}
	return nil
}

func newDaemonsetOVNMetadataAgent(cr *neutronv1beta1.OVNMetadataAgent, cmName string, templatesConfigHash string) *appsv1.DaemonSet {
	var trueVar = true
	daemonSet := appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cr.Namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"daemonset": cr.Name + "-daemonset"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"daemonset": cr.Name + "-daemonset"},
				},
				Spec: corev1.PodSpec{
					NodeSelector:       common.GetComputeWorkerNodeSelector(cr.Spec.RoleName),
					HostNetwork:        true,
					HostPID:            true,
					DNSPolicy:          "ClusterFirstWithHostNet",
					Containers:         []corev1.Container{},
					Tolerations:        []corev1.Toleration{},
					ServiceAccountName: cr.Spec.ServiceAccount,
					PriorityClassName:  "system-node-critical",
				},
			},
		},
	}

	// add compute worker nodes tolerations
	for _, toleration := range common.GetComputeWorkerTolerations(cr.Spec.RoleName) {
		daemonSet.Spec.Template.Spec.Tolerations = append(daemonSet.Spec.Template.Spec.Tolerations, toleration)
	}

	containerSpec := corev1.Container{
		Name:  "ovn-metadata-osp",
		Image: cr.Spec.OVNMetadataAgentImage,
		Command: []string{
			"exec /usr/bin/networking-ovn-metadata-agent --config-file /etc/neutron/neutron.conf --config-file /etc/neutron/plugins/networking-ovn/networking-ovn-metadata-agent.ini --config-dir /etc/neutron/conf.d/networking-ovn-metadata-agent --log-file=/var/log/neutron/ovn-metadata-agent.log",
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: &trueVar,
		},
		Env: []corev1.EnvVar{
			{
				Name:  "TEMPLATES_CONFIG_HASH",
				Value: templatesConfigHash,
			},
		},
		VolumeMounts: []corev1.VolumeMount{},
	}
	// add common VolumeMounts
	for _, volMount := range common.GetVolumeMounts() {
		containerSpec.VolumeMounts = append(containerSpec.VolumeMounts, volMount)
	}
	// add ovnmetadataagent specific VolumeMounts
	for _, volMount := range ovnmetadataagent.GetVolumeMounts(cmName) {
		containerSpec.VolumeMounts = append(containerSpec.VolumeMounts, volMount)
	}

	daemonSet.Spec.Template.Spec.Containers = append(daemonSet.Spec.Template.Spec.Containers, containerSpec)

	// Volume config
	// add common Volumes
	for _, volConfig := range common.GetVolumes(cmName) {
		daemonSet.Spec.Template.Spec.Volumes = append(daemonSet.Spec.Template.Spec.Volumes, volConfig)
	}
	// add ovnmetadataagent Volumes
	for _, volConfig := range ovnmetadataagent.GetVolumes(cmName) {
		daemonSet.Spec.Template.Spec.Volumes = append(daemonSet.Spec.Template.Spec.Volumes, volConfig)
	}

	return &daemonSet
}


func (r *OVNMetadataAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neutronv1beta1.OVNMetadataAgent{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
