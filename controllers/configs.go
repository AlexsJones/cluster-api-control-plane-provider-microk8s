package controllers

import (
	"context"
	"net"
	"strings"
	"time"

	bootstrapv1beta1 "github.com/AlexsJones/cluster-api-bootstrap-provider-microk8s/apis/v1beta1"

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

const (
	dummyConfig string = `
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUREekNDQWZlZ0F3SUJBZ0lVQnNwdzRSbXNNTUlFRHBJU1BHcjlMSHUyZlgwd0RRWUpLb1pJaHZjTkFRRUwKQlFBd0Z6RVZNQk1HQTFVRUF3d01NVEF1TVRVeUxqRTRNeTR4TUI0WERUSXlNRFl4TnpFeU1qVXhPVm9YRFRNeQpNRFl4TkRFeU1qVXhPVm93RnpFVk1CTUdBMVVFQXd3TU1UQXVNVFV5TGpFNE15NHhNSUlCSWpBTkJna3Foa2lHCjl3MEJBUUVGQUFPQ0FROEFNSUlCQ2dLQ0FRRUFxY1lEd3NCbUpMSm1rZXg5bTQ0eCtJMlQ3Q3lvSlFvQ1p4UUEKSGtPRDNiNjhzME9IVWt4dW1YWmUwWHZGOVRQVy9yR0tlb1ZBRUpKMG53eHpEY3M4ZlZ6TXA0UmZ1S1BoL2NmVApOZVJNa2J3WFgxYVgxTXBrTzUxRzdEWm05bG90bVo4Q1V6TnZDaTlwUzBhTzM1WU9EcGh4UGtxT2tYamRuQm5BClhIWFdWUFJqdzZ6UGhYY3dXQVpVMDVlbDhpSFU4Wk1vMlk5eVhaNnhpcWk2Z01lc3p0SlgxQzFVUll6cXRvUU8KeVNrOHNsbUVxMXNRUUQxNjFTTFZsZ3VENy9WSG5LSGZ5bEJyZlFiaUcvcGkxZm5BdnhXYm1ZOG1vc2hpWm4xcwpDazMzbzc2NE93QXk4UFNDVElyNXJnOWJ5TUl2c0h3NkRVZ09qekljSlU2a3V1Z0xRd0lEQVFBQm8xTXdVVEFkCkJnTlZIUTRFRmdRVVBEQUhLYjlGVDdNOEJCREk3WUhtVDNGdmdPWXdId1lEVlIwakJCZ3dGb0FVUERBSEtiOUYKVDdNOEJCREk3WUhtVDNGdmdPWXdEd1lEVlIwVEFRSC9CQVV3QXdFQi96QU5CZ2txaGtpRzl3MEJBUXNGQUFPQwpBUUVBSHZleWQwc3RGaHFjVERPZ09HMHJaWFcwUjNLRGxwVzc2NkZBRGRwOTZ5UGREeG9xZ2dUQktraDBjZ3Z6CmFub2oxR0JmK0ZFWVFuRjlJb29zbXFZWHlFVTNlU05MU2M1NmVpNTFXeWVDa1BROU9RZnZPejlRU2tDT2lVckgKRXhzVzlQRTZXOTBjSVJ6M0h6RXhHeUtFK1JDZUNqZlpDVmNtTUFXMEVrdVVyeHQvY3JJemhxSlFhNUJOV1hGcwo2cEVTWUFJbGtxY0ROZ3lJbjhhVWMxK2hKdVdRamxXeElRc3FnT09GNG56ZjlLTGNCek1sQ0Q0cFc4ekZWRXg0ClhVWEI1NlhyWGdKMzJJbjBxK045dHlXVkN3STZLdUYxak81blhqU0xDQTRQdkpBcmg3YjFsN2lJOGZxR09yaXQKdGFKTlR5N1pwOTFCclBWN3Nha1A1eFQrbXc9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==
    server: https://<HOST>:6443
  name: microk8s-cluster
contexts:
- context:
    cluster: microk8s-cluster
    user: admin
  name: microk8s
current-context: microk8s
kind: Config
preferences: {}
users:
- name: admin
  user:
    client-certificate-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUN4ekNDQWE4Q0ZCNlVJb0ZQSzVTYnBiNWo0ZWNXa3hvYWkzaW5NQTBHQ1NxR1NJYjNEUUVCQ3dVQU1CY3gKRlRBVEJnTlZCQU1NRERFd0xqRTFNaTR4T0RNdU1UQWVGdzB5TWpBMk1UY3hOVEl4TXpkYUZ3MHpPVEV4TURVeApOVEl4TXpkYU1Da3hEakFNQmdOVkJBTU1CV0ZrYldsdU1SY3dGUVlEVlFRS0RBNXplWE4wWlcwNmJXRnpkR1Z5CmN6Q0NBU0l3RFFZSktvWklodmNOQVFFQkJRQURnZ0VQQURDQ0FRb0NnZ0VCQUtqMmZONE8zUkQyWEtPRjJhNlMKUmIxQ3NZckh6eTZ6SFVBWGN6REJKZFYya1hVUWJSRVVaaWtPUkM5c0xnU2NFdFhiOHJpSGxKWG5YWkMzZ3RtUwphNnF4V1MwckFrWTBNREd5SW8xOVlXV2gyZUZvUVZPR1kweFdHeG9kd1BTY0ZoZnhVbVZTbS9ndzlEcFd2MnF3Cm9yV1hLUFlCZU1zeDBrbWdkRnZ0L2hzRXB3ZUt2U3FRYUJ5NGpyYmN2Tk05ZkcrbXJYQ01kY0VDRVRHZGNVTHAKTVB1VTdpcXgyMFkrRWkycWpwTDQwRWxjZVY2MDVzbThrb09mbjhOUFJ0anp5dTYrdFFkb0NRTGJyY2tlZFYyOAp6dmR2b1VJVmlKV25KbmgzaVVLR1F0cUtjMHcrdmVYSXl3blMzL1BMOXUxSmZhb3A0eXZUYjBmNVdQSFRTL244CjI3OENBd0VBQVRBTkJna3Foa2lHOXcwQkFRc0ZBQU9DQVFFQWRrdjB5aS9kZkxXbWIxTU0vMklaYUhXcHpjR1QKb2kyS3lHMFJDOGtHQmxNeHZPYjBSY0d0ck9zdzNqdCtyOFU4ODRkZmV5NjI3cTZGZzdSQUxjWHowNk9aMXRCOQpTL3JGaFdGOHM1QlBJaWd1T2g2RWdMeWVoWitUczNzakZDaWdtZU83QWxVdGV0Q0xvWElNZmkwZG5PMzJJbkdUCndtTWR0aEh3c3pkaE10UGFHNG03YUYycmw1a1RSZlJla3NYd3J1M2ZNalR4MC9jdlU5dTF0MDdJbkVRdW5LaSsKbDBlSlFaVVNYNGx3dkJXb0hvYnZpOXdoMW9oSVJpRFZ1OExYS0xWbUZwQ1BuMG0wTzlxRXRUbnZ3Wi9tSk5DOApOL1VQVTMzVi9DZitEK1c1dC9LdG1GUzErQ0I0WW1DYlNkK3BseFdOaVNaY0orajd2MlRITnVVcHhRPT0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
    client-key-data: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFb2dJQkFBS0NBUUVBcVBaODNnN2RFUFpjbzRYWnJwSkZ2VUt4aXNmUExyTWRRQmR6TU1FbDFYYVJkUkJ0CkVSUm1LUTVFTDJ3dUJKd1MxZHZ5dUllVWxlZGRrTGVDMlpKcnFyRlpMU3NDUmpRd01iSWlqWDFoWmFIWjRXaEIKVTRaalRGWWJHaDNBOUp3V0YvRlNaVktiK0REME9sYS9hckNpdFpjbzlnRjR5ekhTU2FCMFcrMytHd1NuQjRxOQpLcEJvSExpT3R0eTgwejE4YjZhdGNJeDF3UUlSTVoxeFF1a3crNVR1S3JIYlJqNFNMYXFPa3ZqUVNWeDVYclRtCnlieVNnNStmdzA5RzJQUEs3cjYxQjJnSkF0dXR5UjUxWGJ6TzkyK2hRaFdJbGFjbWVIZUpRb1pDMm9welRENjkKNWNqTENkTGY4OHYyN1VsOXFpbmpLOU52Ui9sWThkTkwrZnpidndJREFRQUJBb0lCQUIzVGpVMWgwRko3T3ZVKwozcU43Zk1ZaExOZ3oxM1lGOW1ibS9OV2hjdjFRdGZLMVdKdUlQMVNHQ1RGWjVuRzMzM2RUSVhERHRrNFVEcWRLClRkWDhpL2NRNFk0Z3BvRWdHMVhhZlZEK3poK3p4NU9MNU9SS3QrSzAzSW5xc0xJOWo0VGdlOHdaSGlGYyt2QUYKZWpycVBYN1MxVTlBQ1VQTllyTE9tVnZWRW1OUVVGaElzV2dhZkVaUndiNUI0UEtmK2toOEZLNngzR1duL0xIbgpCWG5XVU50VENGZjkzZ1dwYkFzc29KcjFCVVMzV1AwcFZaTmhzWWZpWWZoNm0xdnlIYUNNRHgyTXJpakhLZGJJClJOTXViWWFlOHZWQ0x3Q0tlOEloOGxLNWgwc2lZNk1YYlIwT2Mrc3Zaa3JJSkdjWmF3aHZFaU9NSnIxclprZ1AKRFVRK1YrRUNnWUVBM21DU2kzVytXZDNNTXgrdkRCUmtPNWJkdTM3alVKdXptZ1grVVFCY0pZa2hGb08vb1czago3blp1YkpoZ2VZUUhSektHT3NrQm1oV05CVFlLV1IxRnV0WEpwdmp6T1JtSXRYRStBTFVLRml3QUw2U0ZRSDcwCjlpK3U2UFBXS0NXMW5laVRxNnMzU0pGNkFWZTRwUERtRThsYXFTOXovWkUyMU42MEFKQS9yNUVDZ1lFQXdvSnYKWjdRU0VGb3ZOSlFSR0t1SVQ0MUE4U3lxRW92elJKRGRLakxZT0h4WW9kWUh0a2duMDZTaDU1MDVCem5qMXhxcgovVGdpRnQyT1V6eHNLbFJyY2RLWXljY2VySVM0L1Rad1YxK2dKQWJBRU5nK2d6THJ0dzJOcFF6UTJuK0NlbEpSCmc2V1JQcUlqM0xtS1hZZ0s4VWlGaHZwYVRkcVBYTVBhSFd5ZnprOENnWUI3K0F4YUVLYXdSSlNNdjVIL1F2THAKd1Y0VkkxU240RlVNZldEY1dUNEZjdC91UkQ0MVNTU3pFSFRZdDAyNUVHQmFVWkZBL2tPVldZUkhMbXd3WjhBeQp1dkh5MG9BTkNlNExjSGpuUGdYRWZIMFNFajV5eVJQWWxwYUVxVUp2R1M2WlBFbnVmc0dRQkFHbTgvY3NoRnRQCkZvWWpJU0FoY0szSGwrdHpFUGRmOFFLQmdEZFRSSDdaMEQySWVWN2FNdGF5aTY0Yy9uamEvSEVVRDVqVUg2Uk8KSEFSTkVpVE9MUmxqQXJrSFhlbjBaWEV4dlNYRTkyQ3FJOEFmT3NsZ0tXQU03UmJPRVJscm9zVHRaM1RXbERPMgpCbVhZNmE2ZzQzOEw3OUg4YitxZmI1U0dxa1ZDdnQ3VUxERUZpMi9QOHBSU0N0TEFqd0pxbVY4RnFMdDVGY1JDCnpsMnZBb0dBWjZnR1FIcEw2YXpvUkhsZlFkK2prdGo4dFpVNU4xaUtmVEFLMENYMk1CdGJHWUpiOEtNM2NhMGEKVms2MFBHYSs1OWhVTHgzblBSaU5maVBPVXdNS2JFcTBOeFhqbjN5YXRxTnBPcWlQUUlDaEdZQjVmN2IwaGZPUQpaM1orOFZQVHB3OTd1QVBVVUdaUmZPdUhQczFGYzA1TElrNGpxOTRjb0VUK0h2Mm1nNTQ9Ci0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg==
`
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

	// See if the kubeconfig exists. If not create it.
	secrets := &corev1.SecretList{}
	err := r.Client.List(ctx, secrets)
	if err != nil {
		return nil, err
	}

	found := false
	for _, s := range secrets.Items {
		if s.Name == cluster.Name+"-kubeconfig" {
			found = true
		}
	}

	c := &clusterv1.Cluster{}
	err = r.Client.Get(ctx, cluster, c)
	if err != nil {
		return nil, err
	}
	if !found && c.Spec.ControlPlaneEndpoint.IsValid() {

		realDummyConfig := strings.Replace(dummyConfig, "<HOST>", c.Spec.ControlPlaneEndpoint.Host, -1)
		configsecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cluster.Namespace,
				Name:      cluster.Name + "-kubeconfig",
			},
			Data: map[string][]byte{
				"value": []byte(realDummyConfig),
			},
		}
		err = r.Client.Create(ctx, configsecret)
		if err != nil {
			return nil, err
		}
	}

	err = r.Client.Get(ctx,
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
