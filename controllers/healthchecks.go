package controllers

import (
	"context"

	clusterv1beta1 "github.com/AlexsJones/cluster-api-control-plane-provider-microk8s/api/v1beta1"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func (r *MicroK8sControlPlaneReconciler) nodesHealthcheck(ctx context.Context, mcp *clusterv1beta1.MicroK8sControlPlane, cluster *clusterv1.Cluster, machines []clusterv1.Machine) error {

	//TODO

	return nil
}

func (r *MicroK8sControlPlaneReconciler) ensureNodesBooted(ctx context.Context,
	mcp *clusterv1beta1.MicroK8sControlPlane, cluster *clusterv1.Cluster, machines []clusterv1.Machine) error {

	//TODO

	return nil
}
