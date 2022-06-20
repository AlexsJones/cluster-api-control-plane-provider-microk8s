package controllers

import (
	"context"

	clusterv1beta1 "github.com/AlexsJones/cluster-api-control-plane-provider-microk8s/api/v1beta1"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
)

func (r *MicroK8sControlPlaneReconciler) updateStatus(ctx context.Context,
	mcp *clusterv1beta1.MicroK8sControlPlane, cluster *clusterv1.Cluster) error {
	clusterSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			clusterv1.ClusterLabelName:             cluster.Name,
			clusterv1.MachineControlPlaneLabelName: "",
		},
	}

	selector, err := metav1.LabelSelectorAsSelector(clusterSelector)
	if err != nil {
		// Since we are building up the LabelSelector above, this should not fail
		return errors.Wrap(err, "failed to parse label selector")
	}
	// Copy label selector to its status counterpart in string format.
	// This is necessary for CRDs including scale subresources.
	mcp.Status.Selector = selector.String()

	ownedMachines, err := r.getControlPlaneMachinesForCluster(ctx, util.ObjectKey(cluster), mcp.Name)
	if err != nil {
		return err
	}

	replicas := int32(len(ownedMachines))

	// set basic data that does not require interacting with the workload cluster
	mcp.Status.Ready = false
	mcp.Status.Replicas = replicas
	mcp.Status.ReadyReplicas = 0
	mcp.Status.UnavailableReplicas = replicas

	// Return early if the deletion timestamp is set, we don't want to try to connect to the workload cluster.
	if !mcp.DeletionTimestamp.IsZero() {
		return nil
	}

	kubeclient, err := r.kubeconfigForCluster(ctx, util.ObjectKey(cluster))
	if err != nil {
		log.Info("failed to get kubeconfig for the cluster", " error ", err)
		return nil
	}

	defer kubeclient.Close() //nolint:errcheck

	nodeSelector := labels.NewSelector()
	req, err := labels.NewRequirement("node.kubernetes.io/microk8s-controlplane", selection.Exists, []string{})
	if err != nil {
		return err
	}

	nodes, err := kubeclient.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: nodeSelector.Add(*req).String(),
	})

	if err != nil {
		log.Info("failed to list controlplane nodes", "error", err)

		return nil
	}

	// TODO: this is ugly and not in the right place. We need a better way to update the ProviderID
	// in each node because MicroK8s is not doing that by default.
	for _, node := range nodes.Items {
		if util.IsNodeReady(&node) {
			log.Info(node.Spec.ProviderID)
			if node.Spec.ProviderID == "" {
				for _, address := range node.Status.Addresses {
					for _, machine := range ownedMachines {
						for _, maddress := range machine.Status.Addresses {
							if maddress.Address == address.Address {
								node.Spec.ProviderID = *machine.Spec.ProviderID
								_, err := kubeclient.CoreV1().Nodes().Update(ctx, &node, metav1.UpdateOptions{})
								if err != nil {
									log.Info("failed to update node", " error ", err)
									return nil
								}
							}
						}
					}
				}
			}

			mcp.Status.ReadyReplicas++
		}
	}

	mcp.Status.UnavailableReplicas = replicas - mcp.Status.ReadyReplicas

	if len(nodes.Items) > 0 {
		mcp.Status.Initialized = true
		conditions.MarkTrue(mcp, clusterv1beta1.AvailableCondition)
	}

	if mcp.Status.ReadyReplicas > 0 {
		mcp.Status.Ready = true
	}

	log.Info("ready replicas", " count ", mcp.Status.ReadyReplicas)

	return nil
}
