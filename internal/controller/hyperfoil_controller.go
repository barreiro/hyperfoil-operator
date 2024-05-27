/*
Copyright 2021.

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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hyperfoilv1alpha2 "github.com/Hyperfoil/hyperfoil-operator/api/v1alpha2"
)

// HyperfoilReconciler reconciles a Hyperfoil object
type HyperfoilReconciler struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	RoutesAvailable bool
}

var routeHost = "load.me"

//+kubebuilder:rbac:groups=hyperfoil.io,resources=hyperfoils,verbs="*"
//+kubebuilder:rbac:groups=hyperfoil.io,resources=hyperfoils/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=hyperfoil.io,resources=hyperfoils/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods;pods/finalizer;pods/log;pods/status;services;configmaps;serviceaccounts;secrets;persistentvolumeclaims,verbs="*"
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;create
//+kubebuilder:rbac:groups=apps,resourceNames=hyperfoil-operator,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs="*"
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes;routes/custom-host,verbs="*"

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *HyperfoilReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("hyperfoil", req.NamespacedName)

	logger.Info("Reconciling Hyperfoil")

	// Fetch the Hyperfoil instance
	instance := &hyperfoilv1alpha2.Hyperfoil{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if k8sErrors.IsNotFound(err) {
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
	if err := ensureSame(r, ctx, instance, logger, controllerRole, "Role",
		&rbacv1.Role{}, nocompare, nocheck); err != nil {
		return reconcile.Result{}, err
	}

	controllerServiceAccount := controllerServiceAccount(instance)
	if err := ensureSame(r, ctx, instance, logger, controllerServiceAccount, "ServiceAccount",
		&corev1.ServiceAccount{}, nocompare, nocheck); err != nil {
		return reconcile.Result{}, err
	}

	controllerRoleBinding := controllerRoleBinding(instance)
	if err := ensureSame(r, ctx, instance, logger, controllerRoleBinding, "RoleBinding",
		&rbacv1.RoleBinding{}, nocompare, nocheck); err != nil {
		return reconcile.Result{}, err
	}

	if r.RoutesAvailable {
		controllerRoute, err := controllerRoute(r, ctx, instance, logger)
		if err != nil {
			return reconcile.Result{}, err
		}
		if err := ensureSame(r, ctx, instance, logger, controllerRoute, "Route",
			&routev1.Route{}, compareControllerRoute, checkControllerRoute); err != nil {
			return reconcile.Result{}, err
		}
		if instance.Spec.Auth.Secret != "" && instance.Spec.Route.Type == "http" {
			updateStatus(r, ctx, instance, "Error", "Auth secret is set but the route is not encrypted.")
			return reconcile.Result{}, errors.New("auth secret is set but the route is not encrypted")
		} else if instance.Spec.Route.Type == "passthrough" && instance.Spec.Route.TLS == "" {
			updateStatus(r, ctx, instance, "Error", "Passthrough route must have TLS secret defined.")
			return reconcile.Result{}, errors.New("passthrough route must have TLS secret defined")
		}

		// This is a hack to workaround not being able to guess the route name ahead
		actualRoute := routev1.Route{}
		err = r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, &actualRoute)
		if err == nil {
			routeHost = actualRoute.Spec.Host
		}
	}

	pvc := corev1.PersistentVolumeClaim{}
	if instance.Spec.PersistentVolumeClaim != "" {
		storage, _ := k8sResource.ParseQuantity("1G")
		pvc = corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      instance.Spec.PersistentVolumeClaim,
				Namespace: instance.Namespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				VolumeName: instance.Spec.PersistentVolumeClaim,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"storage": storage,
					},
				},
			},
		}
		if err := ensureSame(r, ctx, instance, logger, &pvc, "PersistentVolumeClaim",
			&corev1.PersistentVolumeClaim{}, nocompare, nocheck); err != nil {
			return reconcile.Result{}, err
		}
	}

	controllerPod := controllerPod(instance)
	if err := ensureSame(r, ctx, instance, logger, controllerPod, "Pod",
		&corev1.Pod{}, compareControllerPod, checkControllerPod); err != nil {
		return reconcile.Result{}, err
	}

	controllerService := controllerService(instance, r.RoutesAvailable)
	if err := ensureSame(r, ctx, instance, logger, controllerService, "Service",
		&corev1.Service{}, compareControllerService, nocheck); err != nil {
		return reconcile.Result{}, err
	}

	controllerClusterService := controllerClusterService(instance)
	if err := ensureSame(r, ctx, instance, logger, controllerClusterService, "Service",
		&corev1.Service{}, nocompare, nocheck); err != nil {
		return reconcile.Result{}, err
	}

	r.Status().Update(ctx, instance)

	return ctrl.Result{}, nil
}

func setStatus(instance *hyperfoilv1alpha2.Hyperfoil, status string, reason string) {
	if instance.Status.Status == "Error" && status == "Pending" {
		return
	}
	instance.Status.Status = status
	instance.Status.Reason = reason
	instance.Status.LastUpdate = metav1.Now()
}

func updateStatus(r *HyperfoilReconciler, ctx context.Context, instance *hyperfoilv1alpha2.Hyperfoil, status string, reason string) {
	setStatus(instance, status, reason)
	r.Status().Update(ctx, instance)
}

type resource interface {
	metav1.Object
	runtime.Object
}

func ensureSame(r *HyperfoilReconciler, ctx context.Context, instance *hyperfoilv1alpha2.Hyperfoil, logger logr.Logger,
	object resource, resourceType string, out client.Object,
	compare func(interface{}, interface{}, logr.Logger) bool,
	check func(interface{}) (bool, string, string)) error {
	// Set Hyperfoil instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, object, r.Scheme); err != nil {
		return err
	}

	// Check if this resource already exists
	err := r.Get(ctx, types.NamespacedName{Name: object.GetName(), Namespace: object.GetNamespace()}, out)
	if err != nil && k8sErrors.IsNotFound(err) {
		logger.Info("Creating a new "+resourceType, resourceType+".Namespace", object.GetNamespace(), resourceType+".Name", object.GetName())
		err = r.Create(ctx, object)
		if err != nil {
			bytes, _ := json.MarshalIndent(object, "", "  ")
			logger.Info("Failed object: " + string(bytes))
			updateStatus(r, ctx, instance, "Error", "Cannot create "+resourceType+" "+object.GetName())
			return err
		}
		setStatus(instance, "Pending", "Creating "+resourceType+" "+object.GetName())
	} else if err != nil {
		updateStatus(r, ctx, instance, "Error", "Cannot find "+resourceType+" "+object.GetName())
		return err
	} else if compare(object, out, logger) {
		logger.Info(resourceType + " " + object.GetName() + " already exists and matches.")
		if ok, status, reason := check(out); !ok {
			setStatus(instance, status, resourceType+" "+object.GetName()+" "+reason)
		}
	} else {
		logger.Info(resourceType + " " + object.GetName() + " already exists but does not match. Deleting existing object.")
		if err = r.Delete(ctx, out); err != nil {
			logger.Error(err, "Cannot delete "+resourceType+" "+object.GetName())
			updateStatus(r, ctx, instance, "Error", "Cannot delete "+resourceType+" "+object.GetName())
			return err
		}
		logger.Info("Creating a new " + resourceType)
		if err = r.Create(ctx, object); err != nil {
			updateStatus(r, ctx, instance, "Error", "Cannot create "+resourceType+" "+object.GetName())
			return err
		}
		setStatus(instance, "Pending", "Creating "+resourceType+" "+object.GetName())
	}
	return nil
}

func controllerRole(cr *hyperfoilv1alpha2.Hyperfoil) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller",
			Namespace: cr.Namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Verbs: []string{
					"*",
				},
				Resources: []string{
					"pods", "pods/log", "pods/status", "pods/finalizer",
				},
			},
		},
	}
}

func controllerServiceAccount(cr *hyperfoilv1alpha2.Hyperfoil) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller",
			Namespace: cr.Namespace,
		},
	}
}

func controllerRoleBinding(cr *hyperfoilv1alpha2.Hyperfoil) *rbacv1.RoleBinding {
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
			{
				Kind:      "ServiceAccount",
				Name:      "controller",
				Namespace: cr.Namespace,
			},
		},
	}
}

func controllerPod(cr *hyperfoilv1alpha2.Hyperfoil) *corev1.Pod {
	labels := map[string]string{
		"app":  cr.Name,
		"role": "controller",
	}
	imagePullPolicy := corev1.PullIfNotPresent
	version := "latest"
	if cr.Spec.Version != "" {
		version = cr.Spec.Version
		imagePullPolicy = corev1.PullAlways
	}
	image := "quay.io/hyperfoil/hyperfoil:" + version
	if cr.Spec.Image != "" {
		image = cr.Spec.Image
		imagePullPolicy = corev1.PullAlways
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
	var externalURI string
	var protocol string
	if cr.Spec.Route.Type == "http" {
		protocol = "http"
	} else {
		protocol = "https"
	}
	if cr.Spec.Route.Host != "" {
		externalURI = protocol + "://" + cr.Spec.Route.Host
	} else {
		externalURI = protocol + "://" + routeHost
	}
	command := []string{
		"/deployment/bin/controller.sh",
		"-Dio.hyperfoil.deploy.timeout=" + strconv.Itoa(deployTimeout),
		"-Dio.hyperfoil.deployer=k8s",
		"-Dio.hyperfoil.deployer.k8s.namespace=" + cr.Namespace,
		"-Dio.hyperfoil.controller.host=0.0.0.0",
		"-Dio.hyperfoil.controller.external.uri=" + externalURI,
		"-Dio.hyperfoil.rootdir=/var/hyperfoil/",
	}
	command = append(command, cr.Spec.AdditionalArgs...)

	volumes := []corev1.Volume{}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "hyperfoil",
			MountPath: "/var/hyperfoil",
		},
	}
	if cr.Spec.PersistentVolumeClaim != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "hyperfoil",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cr.Spec.PersistentVolumeClaim,
				},
			},
		})
	} else {
		volumes = append(volumes, corev1.Volume{
			Name: "hyperfoil",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
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
	if len(cr.Spec.PreHooks) > 0 {
		volumes = addProjectedConfigMapsVolume(volumes, "prehooks", cr.Spec.PreHooks, 0755)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "prehooks",
			MountPath: "/var/hyperfoil/hooks/pre",
			ReadOnly:  true,
		})
	}
	if len(cr.Spec.PostHooks) > 0 {
		volumes = addProjectedConfigMapsVolume(volumes, "posthooks", cr.Spec.PostHooks, 0755)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "posthooks",
			MountPath: "/var/hyperfoil/hooks/post",
			ReadOnly:  true,
		})
	}
	if cr.Spec.TriggerURL != "" {
		command = append(command, "-Dio.hyperfoil.trigger.url="+cr.Spec.TriggerURL)
	}

	envVars := make([]corev1.EnvVar, 0)
	if cr.Spec.Auth.Secret != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name: "IO_HYPERFOIL_CONTROLLER_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cr.Spec.Auth.Secret,
					},
					Key: "password",
				},
			},
		})
	}
	envFrom := make([]corev1.EnvFromSource, 0)
	for _, secret := range cr.Spec.SecretEnvVars {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secret,
				},
			},
		})
	}

	if cr.Spec.Route.Type == "reencrypt" || cr.Spec.Route.Type == "passthrough" {
		// for reencrypt routes the certificate is generated by an annotation at the service
		secret := cr.Name
		if cr.Spec.Route.Type == "passthrough" {
			secret = cr.Spec.Route.TLS
		}
		volumes = append(volumes, corev1.Volume{
			Name: "certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secret,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "certs",
			MountPath: "/var/hyperfoil/certs/",
			ReadOnly:  true,
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "IO_HYPERFOIL_CONTROLLER_PEM_CERTS",
			Value: "/var/hyperfoil/certs/" + corev1.TLSCertKey,
		}, corev1.EnvVar{
			Name:  "IO_HYPERFOIL_CONTROLLER_PEM_KEYS",
			Value: "/var/hyperfoil/certs/" + corev1.TLSPrivateKeyKey,
		})
	} else if cr.Spec.Route.Type == "edge" || cr.Spec.Route.Type == "" {
		command = append(command, "-Dio.hyperfoil.controller.secured.via.proxy=true")
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-controller",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &[]int64{0}[0],
			Containers: []corev1.Container{
				{
					Name:            "controller",
					Image:           image,
					Command:         command,
					VolumeMounts:    volumeMounts,
					ImagePullPolicy: imagePullPolicy,
					Env:             envVars,
					EnvFrom:         envFrom,
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

func addProjectedConfigMapsVolume(volumes []corev1.Volume, name string, configMaps []string, mode int32) []corev1.Volume {
	sources := make([]corev1.VolumeProjection, 0)
	for _, configMap := range configMaps {
		var items []corev1.KeyToPath = nil
		if strings.Contains(configMap, "/") {
			parts := strings.SplitN(configMap, "/", 2)
			items = make([]corev1.KeyToPath, 1)
			configMap, items[0] = parts[0], corev1.KeyToPath{
				Key:  parts[1],
				Path: parts[1],
			}
		}
		sources = append(sources, corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMap,
				},
				Items: items,
			},
		})
	}
	return append(volumes, corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				DefaultMode: &mode,
				Sources:     sources,
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

	// Compare for equality only whatever field is set in p1.Spec.
	if equality.Semantic.DeepDerivative(p1.Spec, p2.Spec) {
		return true
	}

	diff := cmp.Diff(p1.Spec, p2.Spec)
	logger.Info("Pod " + p1.GetName() + " diff (-want,+got):\n" + diff)
	return false
}

func checkControllerPod(i interface{}) (bool, string, string) {
	pod, ok := i.(*corev1.Pod)
	if !ok {
		return false, "Error", " is not a pod"
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if reason == "ImagePullBackOff" || reason == "ErrImagePull" {
				return false, "Error", " cannot pull container image"
			}
		} else if cs.State.Terminated != nil {
			return false, "Pending", " has terminated container"
		}
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true, "", ""
		}
	}
	return false, "Pending", " is not ready"
}

func controllerService(cr *hyperfoilv1alpha2.Hyperfoil, defaultClusterIP bool) *corev1.Service {
	var serviceType = cr.Spec.ServiceType
	if serviceType == "" {
		if defaultClusterIP {
			serviceType = corev1.ServiceTypeClusterIP
		} else {
			serviceType = corev1.ServiceTypeNodePort
		}
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
			Annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": cr.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				"app":  cr.Name,
				"role": "controller",
			},
			Ports: []corev1.ServicePort{
				{
					Port: int32(8090),
					TargetPort: intstr.IntOrString{
						StrVal: "8090-8090",
						IntVal: 8090,
					},
				},
			},
		},
	}
}

func compareControllerService(i1 interface{}, i2 interface{}, logger logr.Logger) bool {
	s1, ok1 := i1.(*corev1.Service)
	s2, ok2 := i2.(*corev1.Service)
	if !ok1 || !ok2 {
		logger.Info("Cannot cast to Services: " + fmt.Sprintf("%v | %v", i1, i2))
		return false
	}

	// DeepDerivative doesn't work here because the NodePort is 0 by default (and set in retrieved object)
	// Therefore we check just the correct type...
	if s1.Spec.Type == s2.Spec.Type {
		return true
	}

	diff := cmp.Diff(s1.Spec, s2.Spec)
	logger.Info("Service " + s1.GetName() + " diff (-want,+got):\n" + diff)
	return false
}

func controllerClusterService(cr *hyperfoilv1alpha2.Hyperfoil) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-cluster",
			Namespace: cr.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "None",
			Selector: map[string]string{
				"app":  cr.Name,
				"role": "controller",
			},
			Ports: []corev1.ServicePort{
				{
					Port: int32(7800),
					TargetPort: intstr.IntOrString{
						StrVal: "7800-7800",
						IntVal: 7800,
					},
				},
			},
		},
	}
}

func controllerRoute(r *HyperfoilReconciler, ctx context.Context, cr *hyperfoilv1alpha2.Hyperfoil, logger logr.Logger) (*routev1.Route, error) {
	subdomain := ""
	if cr.Spec.Route.Host == "" {
		subdomain = cr.Name
		if cr.Namespace != "" {
			subdomain += "-" + cr.Namespace
		}
	}
	tls, err := tls(r, ctx, cr, logger)
	if err != nil {
		return nil, err
	}
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
			Labels: map[string]string{
				"hyperfoil": cr.Name,
			},
		},
		Spec: routev1.RouteSpec{
			Host: cr.Spec.Route.Host,
			// If the Host is not set (empty) we'll use CR's name
			Subdomain: subdomain,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: cr.Name,
			},
			TLS: tls,
		},
	}, nil
}

func tls(r *HyperfoilReconciler, ctx context.Context, cr *hyperfoilv1alpha2.Hyperfoil, logger logr.Logger) (*routev1.TLSConfig, error) {
	switch cr.Spec.Route.Type {
	case "http":
		return nil, nil
	case "passthrough":
		return &routev1.TLSConfig{
			Termination:                   routev1.TLSTerminationPassthrough,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		}, nil
	}
	tlsSecret := corev1.Secret{}
	if cr.Spec.Route.TLS != "" {
		if error := r.Get(ctx, types.NamespacedName{Name: cr.Spec.Route.TLS, Namespace: cr.Namespace}, &tlsSecret); error != nil {
			updateStatus(r, ctx, cr, "Error", "Cannot find secret "+cr.Spec.Route.TLS)
			return nil, error
		}
	}
	cacert := ""
	if bytes, ok := tlsSecret.Data["ca.crt"]; ok {
		cacert = string(bytes)
	}
	var termination routev1.TLSTerminationType
	switch cr.Spec.Route.Type {
	case "edge", "":
		termination = routev1.TLSTerminationEdge
	case "passthrough":
		termination = routev1.TLSTerminationPassthrough
	case "reencrypt":
		termination = routev1.TLSTerminationReencrypt
	default:
		logger.Info("Invalid route type: " + cr.Spec.Route.Type)
		return nil, errors.New("Invalid route type: " + cr.Spec.Route.Type)
	}
	return &routev1.TLSConfig{
		Termination:                   termination,
		InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		Certificate:                   string(tlsSecret.Data[corev1.TLSCertKey]),
		Key:                           string(tlsSecret.Data[corev1.TLSPrivateKeyKey]),
		CACertificate:                 cacert,
	}, nil
}

func compareControllerRoute(i1 interface{}, i2 interface{}, logger logr.Logger) bool {
	r1, ok1 := i1.(*routev1.Route)
	r2, ok2 := i2.(*routev1.Route)
	if !ok1 || !ok2 {
		logger.Info("Cannot cast to Routes: " + fmt.Sprintf("%v | %v", i1, i2))
		return false
	}

	// Compare for equality only whatever field is set in r1.Spec.
	if equality.Semantic.DeepDerivative(r1.Spec, r2.Spec) {
		return true
	}

	diff := cmp.Diff(r1.Spec, r2.Spec)
	logger.Info("Route " + r1.GetName() + " diff (-want, +got):\n" + diff)

	return false
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
				}
				return false, "Error", " was not admitted"
			}
		}
	}
	return false, "Pending", " is in unknown state"
}

// SetupWithManager sets up the controller with the Manager.
func (r *HyperfoilReconciler) SetupWithManager(mgr ctrl.Manager) error {
	controller := ctrl.NewControllerManagedBy(mgr).
		For(&hyperfoilv1alpha2.Hyperfoil{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{})
	if r.RoutesAvailable {
		controller = controller.Owns(&routev1.Route{})
	}
	return controller.Complete(r)
}
