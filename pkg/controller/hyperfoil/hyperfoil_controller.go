package hyperfoil

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	hyperfoilv1alpha1 "github.com/Hyperfoil/hyperfoil-operator/pkg/apis/hyperfoil/v1alpha1"
	version "github.com/Hyperfoil/hyperfoil-operator/version"
	logr "github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_hyperfoil")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Hyperfoil Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileHyperfoil{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("hyperfoil-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	if err = routev1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	if err = rbacv1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	// Watch for changes to primary resource Hyperfoil
	err = c.Watch(&source.Kind{Type: &hyperfoilv1alpha1.Hyperfoil{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Pods and requeue the owner Hyperfoil
	if err = watchSecondary(c, &corev1.Pod{}); err != nil {
		return err
	}
	if err = watchSecondary(c, &corev1.Service{}); err != nil {
		return err
	}
	if err = watchSecondary(c, &routev1.Route{}); err != nil {
		return err
	}
	if err = watchSecondary(c, &rbacv1.Role{}); err != nil {
		return err
	}
	if err = watchSecondary(c, &rbacv1.RoleBinding{}); err != nil {
		return err
	}
	if err = watchSecondary(c, &corev1.ServiceAccount{}); err != nil {
		return err
	}
	return nil
}

func watchSecondary(c controller.Controller, typ runtime.Object) error {
	return c.Watch(&source.Kind{Type: typ}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &hyperfoilv1alpha1.Hyperfoil{},
	})
}

// blank assignment to verify that ReconcileHyperfoil implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileHyperfoil{}

// ReconcileHyperfoil reconciles a Hyperfoil object
type ReconcileHyperfoil struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Hyperfoil object and makes changes based on the state read
// and what is in the Hyperfoil.Spec
func (r *ReconcileHyperfoil) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	logger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	logger.Info("Reconciling Hyperfoil")

	// Fetch the Hyperfoil instance
	instance := &hyperfoilv1alpha1.Hyperfoil{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	if instance.Status.Status != "Ready" {
		instance.Status.Status = "Ready"
		instance.Status.Reason = "Reconciliation succeeded."
		instance.Status.LastUpdate = metav1.Now()
	}

	nocompare := func(interface{}, interface{}, logr.Logger) bool {
		return true
	}
	nocheck := func(interface{}) (bool, string, string) {
		return true, "", ""
	}

	controllerRole := controllerRole(instance)
	if err := ensureSame(r, instance, logger, controllerRole, "Role",
		&rbacv1.Role{}, nocompare, nocheck); err != nil {
		return reconcile.Result{}, err
	}

	controllerServiceAccount := controllerServiceAccount(instance)
	if err := ensureSame(r, instance, logger, controllerServiceAccount, "ServiceAccount",
		&corev1.ServiceAccount{}, nocompare, nocheck); err != nil {
		return reconcile.Result{}, err
	}

	controllerRoleBinding := controllerRoleBinding(instance)
	if err := ensureSame(r, instance, logger, controllerRoleBinding, "RoleBinding",
		&rbacv1.RoleBinding{}, nocompare, nocheck); err != nil {
		return reconcile.Result{}, err
	}

	controllerPod := controllerPod(instance)
	if err := ensureSame(r, instance, logger, controllerPod, "Pod",
		&corev1.Pod{}, compareControllerPod, checkControllerPod); err != nil {
		return reconcile.Result{}, err
	}

	controllerService := controllerService(instance)
	if err := ensureSame(r, instance, logger, controllerService, "Service",
		&corev1.Service{}, nocompare, nocheck); err != nil {
		return reconcile.Result{}, err
	}

	controllerRoute := controllerRoute(instance)
	if err := ensureSame(r, instance, logger, controllerRoute, "Route",
		&routev1.Route{}, compareControllerRoute, checkControllerRoute); err != nil {
		return reconcile.Result{}, err
	}

	r.client.Status().Update(context.TODO(), instance)

	return reconcile.Result{}, nil
}

func setStatus(r *ReconcileHyperfoil, instance *hyperfoilv1alpha1.Hyperfoil, status string, reason string) {
	if instance.Status.Status == "Error" && status == "Pending" {
		return
	}
	instance.Status.Status = status
	instance.Status.Reason = reason
	instance.Status.LastUpdate = metav1.Now()
}

func updateStatus(r *ReconcileHyperfoil, instance *hyperfoilv1alpha1.Hyperfoil, status string, reason string) {
	setStatus(r, instance, status, reason)
	r.client.Status().Update(context.TODO(), instance)
}

type resource interface {
	metav1.Object
	runtime.Object
}

func ensureSame(r *ReconcileHyperfoil, instance *hyperfoilv1alpha1.Hyperfoil, logger logr.Logger,
	object resource, resourceType string, out runtime.Object,
	compare func(interface{}, interface{}, logr.Logger) bool,
	check func(interface{}) (bool, string, string)) error {
	// Set Hyperfoil instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, object, r.scheme); err != nil {
		return err
	}

	// Check if this Pod already exists
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: object.GetName(), Namespace: object.GetNamespace()}, out)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating a new "+resourceType, resourceType+".Namespace", object.GetNamespace(), resourceType+".Name", object.GetName())
		err = r.client.Create(context.TODO(), object)
		if err != nil {
			updateStatus(r, instance, "Error", "Cannot create "+resourceType+" "+object.GetName())
			return err
		}
		setStatus(r, instance, "Pending", "Creating "+resourceType+" "+object.GetName())
	} else if err != nil {
		updateStatus(r, instance, "Error", "Cannot find "+resourceType+" "+object.GetName())
		return err
	} else if compare(object, out, logger) {
		logger.Info(resourceType + " " + object.GetName() + " already exists and matches.")
		if ok, status, reason := check(out); !ok {
			setStatus(r, instance, status, resourceType+" "+object.GetName()+" "+reason)
		}
	} else {
		logger.Info(resourceType + " " + object.GetName() + " already exists but does not match. Deleting existing object.")
		if err = r.client.Delete(context.TODO(), out); err != nil {
			logger.Error(err, "Cannot delete "+resourceType+" "+object.GetName())
			updateStatus(r, instance, "Error", "Cannot delete "+resourceType+" "+object.GetName())
			return err
		}
		logger.Info("Creating a new " + resourceType)
		if err = r.client.Create(context.TODO(), object); err != nil {
			updateStatus(r, instance, "Error", "Cannot create "+resourceType+" "+object.GetName())
			return err
		}
		setStatus(r, instance, "Pending", "Creating "+resourceType+" "+object.GetName())
	}
	return nil
}

func controllerRole(cr *hyperfoilv1alpha1.Hyperfoil) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller",
			Namespace: cr.Namespace,
		},
		Rules: []rbacv1.PolicyRule{
			rbacv1.PolicyRule{
				APIGroups: []string{""},
				Verbs: []string{
					"create", "delete", "watch",
				},
				Resources: []string{
					"pods",
				},
			},
		},
	}
}

func controllerServiceAccount(cr *hyperfoilv1alpha1.Hyperfoil) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller",
			Namespace: cr.Namespace,
		},
	}
}

func controllerRoleBinding(cr *hyperfoilv1alpha1.Hyperfoil) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller",
			Namespace: cr.Namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "controller",
		},
		Subjects: []rbacv1.Subject{
			rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      "controller",
				Namespace: cr.Namespace,
			},
		},
	}
}

func controllerPod(cr *hyperfoilv1alpha1.Hyperfoil) *corev1.Pod {
	labels := map[string]string{
		"app":  cr.Name,
		"role": "controller",
	}
	version := version.Version
	if cr.Spec.Version != "" {
		version = cr.Spec.Version
	}
	image := "quay.io/hyperfoil/hyperfoil:" + version
	if cr.Spec.Image != "" {
		image := cr.Spec.Image
		if cr.Spec.Version != "" {
			if strings.Contains(image, ":") {
				image = strings.Split(image, ":")[0] + ":" + cr.Spec.Version
			} else {
				image = image + ":" + cr.Spec.Version
			}
		}
	}
	deployTimeout := 120000
	if cr.Spec.AgentDeployTimeout != 0 {
		deployTimeout = cr.Spec.AgentDeployTimeout
	}
	command := []string{
		"/deployment/bin/controller.sh",
		"-Dio.hyperfoil.deploy.timeout=" + strconv.Itoa(deployTimeout),
		"-Dio.hyperfoil.deployer=k8s",
		"-Dio.hyperfoil.controller.host=0.0.0.0",
		"-Dio.hyperfoil.rootdir=/var/hyperfoil/",
	}
	volumes := []corev1.Volume{
		corev1.Volume{
			Name: "hyperfoil",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
	volumeMounts := []corev1.VolumeMount{
		corev1.VolumeMount{
			Name:      "hyperfoil",
			MountPath: "/var/hyperfoil",
		},
	}
	if cr.Spec.Log != "" {
		configMap := cr.Spec.Log
		file := "log4j2.xml"
		if strings.Contains(cr.Spec.Log, "/") {
			parts := strings.SplitN(cr.Spec.Log, "/", 2)
			configMap, file = parts[0], parts[1]
		}
		command = append(command, "-Dlog4j.configurationFile=file:///etc/log4j2/"+file)
		volumes = addConfigMapVolume(volumes, "log", configMap, false, 0644)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "log",
			MountPath: "/etc/log4j2/",
			ReadOnly:  true,
		})
	}
	if cr.Spec.PreHooks != "" {
		volumes = addConfigMapVolume(volumes, "prehooks", cr.Spec.PreHooks, false, 0755)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "prehooks",
			MountPath: "/var/hyperfoil/hooks/pre",
			ReadOnly:  true,
		})
	}
	if cr.Spec.PostHooks != "" {
		volumes = addConfigMapVolume(volumes, "posthooks", cr.Spec.PostHooks, false, 0755)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "posthooks",
			MountPath: "/hyperfoil/hooks/post",
			ReadOnly:  true,
		})
	}
	if cr.Spec.TriggerURL != "" {
		command = append(command, "-Dio.hyperfoil.trigger.url="+cr.Spec.TriggerURL)
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-controller",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:         "controller",
					Image:        image,
					Command:      command,
					VolumeMounts: volumeMounts,
				},
			},
			Volumes:            volumes,
			ServiceAccountName: "controller",
		},
	}
}

func addConfigMapVolume(volumes []corev1.Volume, name string, configMap string, optional bool, mode int32) []corev1.Volume {
	return append(volumes, corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMap,
				},
				Optional:    &optional,
				DefaultMode: &mode,
			},
		},
	})
}

func compareControllerPod(i1 interface{}, i2 interface{}, logger logr.Logger) bool {
	p1, ok1 := i1.(*corev1.Pod)
	p2, ok2 := i2.(*corev1.Pod)
	if !ok1 || !ok2 {
		logger.Info("Cannot cast to Pods: " + fmt.Sprintf("%v | %v", i1, i2))
		return false
	}
	c1 := p1.Spec.Containers[0]
	c2 := p2.Spec.Containers[0]
	if c1.Image != c2.Image {
		logger.Info("Images don't match: " + c1.Image + " | " + c2.Image)
		return false
	}
	if !reflect.DeepEqual(c1.Command, c2.Command) {
		logger.Info("Commands don't match: " + fmt.Sprintf("%v | %v", c1.Command, c2.Command))
		return false
	}
	if !compareVolume(p1.Spec.Volumes, p2.Spec.Volumes, "log", logger) ||
		!compareVolume(p1.Spec.Volumes, p2.Spec.Volumes, "prehook", logger) ||
		!compareVolume(p1.Spec.Volumes, p2.Spec.Volumes, "posthook", logger) {
		return false
	}

	return true
}

func checkControllerPod(i interface{}) (bool, string, string) {
	pod, ok := i.(*corev1.Pod)
	if !ok {
		return false, "Error", " is not a pod"
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true, "", ""
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if reason == "ImagePullBackOff" || reason == "ErrImagePull" {
				return false, "Error", " cannot pull container image"
			}
		}
	}
	return false, "Pending", " is not ready"
}

func findVolume(volumes []corev1.Volume, name string) (corev1.Volume, bool) {
	for _, v := range volumes {
		if v.Name == name {
			return v, true
		}
	}
	return corev1.Volume{}, false
}

func compareVolume(vs1 []corev1.Volume, vs2 []corev1.Volume, name string, logger logr.Logger) bool {
	v1, h1 := findVolume(vs1, name)
	v2, h2 := findVolume(vs2, name)
	if h1 != h2 {
		logger.Info("One of the pods has volume " + name + ", other does not")
		return false
	}
	if h1 && h2 {
		n1 := v1.VolumeSource.ConfigMap.LocalObjectReference.Name
		n2 := v2.VolumeSource.ConfigMap.LocalObjectReference.Name
		if n1 != n2 {
			logger.Info("Names of ConfigMaps for volume " + name + " don't match: " + n1 + " | " + n2)
			return false
		}
	}
	return true
}

func controllerService(cr *hyperfoilv1alpha1.Hyperfoil) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app":  cr.Name,
				"role": "controller",
			},
			Ports: []corev1.ServicePort{
				corev1.ServicePort{
					Port: int32(8090),
					TargetPort: intstr.IntOrString{
						StrVal: "8090-8090",
					},
				},
			},
		},
	}
}

func controllerRoute(cr *hyperfoilv1alpha1.Hyperfoil) *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		},
		Spec: routev1.RouteSpec{
			Host: cr.Spec.Route,
			// If the Host is not set (empty) we'll use CR's name
			Subdomain: cr.Name,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: cr.Name,
			},
		},
	}
}

func compareControllerRoute(i1 interface{}, i2 interface{}, logger logr.Logger) bool {
	r1, ok1 := i1.(*routev1.Route)
	r2, ok2 := i2.(*routev1.Route)
	if !ok1 || !ok2 {
		logger.Info("Cannot cast to Routes: " + fmt.Sprintf("%v | %v", i1, i2))
		return false
	}
	if r1.Spec.Host != "" && r1.Spec.Host != r2.Spec.Host {
		return false
	}
	return true
}

func checkControllerRoute(i interface{}) (bool, string, string) {
	route, ok := i.(*routev1.Route)
	if !ok {
		return false, "Error", " is not a route"
	}
	for _, ri := range route.Status.Ingress {
		for _, c := range ri.Conditions {
			if c.Type == routev1.RouteAdmitted {
				if c.Status == corev1.ConditionTrue {
					return true, "", ""
				} else {
					return false, "Error", " was not admitted"
				}
			}
		}
	}
	return false, "Pending", " is in unknown state"
}
