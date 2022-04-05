package controllers

import (
	"context"
	"net"
	"time"

	bootstrapv1beta1 "github.com/AlexsJones/cluster-api-bootstrap-provider-microk8s/api/v1beta1"
	clusterv1beta1 "github.com/AlexsJones/cluster-api-control-plane-provider-microk8s/api/v1beta1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/connrotation"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type kubernetesClient struct {
	*kubernetes.Clientset

	dialer *connrotation.Dialer
}

// Close kubernetes client.
func (k *kubernetesClient) Close() error {
	k.dialer.CloseAll()

	return nil
}

func newDialer() *connrotation.Dialer {
	return connrotation.NewDialer((&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext)
}

// kubeconfigForCluster will fetch a kubeconfig secret based on cluster name/namespace,
// use it to create a clientset, and return it.
func (r *MicroK8sControlPlaneReconciler) kubeconfigForCluster(ctx context.Context, cluster client.ObjectKey) (*kubernetesClient, error) {
	kubeconfigSecret := &corev1.Secret{}

	err := r.Client.Get(ctx,
		types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name + "-kubeconfig",
		},
		kubeconfigSecret,
	)
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigSecret.Data["value"])
	if err != nil {
		return nil, err
	}

	dialer := newDialer()
	config.Dial = dialer.DialContext

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &kubernetesClient{
		Clientset: clientset,
		dialer:    dialer,
	}, nil
}

func (r *MicroK8sControlPlaneReconciler) generateMicroK8sConfig(ctx context.Context, tcp *clusterv1beta1.MicroK8sControlPlane,
	cluster *clusterv1.Cluster, spec *bootstrapv1beta1.MicroK8sConfigSpec) (*corev1.ObjectReference, error) {
	owner := metav1.OwnerReference{
		APIVersion:         clusterv1beta1.GroupVersion.String(),
		Kind:               "MicroK8sControlPlane",
		Name:               tcp.Name,
		UID:                tcp.UID,
		BlockOwnerDeletion: pointer.BoolPtr(true),
	}

	bootstrapConfig := &bootstrapv1beta1.MicroK8sConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.SimpleNameGenerator.GenerateName(tcp.Name + "-"),
			Namespace:       tcp.Namespace,
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Spec: *spec,
	}

	if err := r.Client.Create(ctx, bootstrapConfig); err != nil {
		return nil, errors.Wrap(err, "Failed to create bootstrap configuration")
	}

	bootstrapRef := &corev1.ObjectReference{
		APIVersion: bootstrapv1beta1.GroupVersion.String(),
		Kind:       "MicroK8sConfig",
		Name:       bootstrapConfig.GetName(),
		Namespace:  bootstrapConfig.GetNamespace(),
		UID:        bootstrapConfig.GetUID(),
	}

	return bootstrapRef, nil
}
