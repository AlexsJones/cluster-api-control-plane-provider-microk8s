package controllers

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"time"

	clusterv1beta1 "github.com/AlexsJones/cluster-api-control-plane-provider-microk8s/api/v1beta1"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/storage/names"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type errServiceUnhealthy struct {
	service string
	reason  string
}

func (e *errServiceUnhealthy) Error() string {
	return fmt.Sprintf("Service %s is unhealthy: %s", e.service, e.reason)
}

func (r *MicroK8sControlPlaneReconciler) reconcile(ctx context.Context,
	cluster *clusterv1.Cluster, tcp *clusterv1beta1.MicroK8sControlPlane) (res ctrl.Result, err error) {
	log.Info("reconcile MicroK8sControlPlane")

	// Update ownerrefs on infra templates
	if err := r.reconcileExternalReference(ctx, tcp.Spec.InfrastructureTemplate, cluster); err != nil {
		return ctrl.Result{}, err
	}

	// If ControlPlaneEndpoint is not set, return early
	if !cluster.Spec.ControlPlaneEndpoint.IsValid() {
		log.Info("cluster does not yet have a ControlPlaneEndpoint defined")
		return ctrl.Result{}, nil
	}

	// TODO: handle proper adoption of Machines
	ownedMachines, err := r.getControlPlaneMachinesForCluster(ctx, util.ObjectKey(cluster), tcp.Name)
	if err != nil {
		log.Error(err, "failed to retrieve control plane machines for cluster")
		return ctrl.Result{}, err
	}

	conditionGetters := make([]conditions.Getter, len(ownedMachines))

	for i, v := range ownedMachines {
		conditionGetters[i] = &v
	}

	conditions.SetAggregate(tcp, clusterv1beta1.MachinesReadyCondition,
		conditionGetters, conditions.AddSourceRef(), conditions.WithStepCounterIf(false))

	var (
		errs        error
		result      ctrl.Result
		phaseResult ctrl.Result
	)

	// run all similar reconcile steps in the loop and pick the lowest RetryAfter, aggregate errors and check the requeue flags.
	for _, phase := range []func(context.Context, *clusterv1.Cluster, *clusterv1beta1.MicroK8sControlPlane,
		[]clusterv1.Machine) (ctrl.Result, error){
		// r.reconcileEtcdMembers,
		r.reconcileNodeHealth,
		r.reconcileConditions,
		// r.reconcileKubeconfig,
		r.reconcileMachines,
	} {
		phaseResult, err = phase(ctx, cluster, tcp, ownedMachines)
		if err != nil {
			errs = kerrors.NewAggregate([]error{errs, err})
		}

		result = util.LowestNonZeroResult(result, phaseResult)
	}

	if !result.Requeue {
		conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
	}

	return result, errs
}

func (r *MicroK8sControlPlaneReconciler) reconcileNodeHealth(ctx context.Context, cluster *clusterv1.Cluster, mcp *clusterv1beta1.MicroK8sControlPlane, machines []clusterv1.Machine) (result ctrl.Result, err error) {
	if err := r.nodesHealthcheck(ctx, mcp, cluster, machines); err != nil {
		reason := clusterv1beta1.ControlPlaneComponentsInspectionFailedReason

		if errors.Is(err, &errServiceUnhealthy{}) {
			reason = clusterv1beta1.ControlPlaneComponentsUnhealthyReason
		}

		conditions.MarkFalse(mcp, clusterv1beta1.ControlPlaneComponentsHealthyCondition, reason,
			clusterv1.ConditionSeverityWarning, err.Error())

		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	} else {
		conditions.MarkTrue(mcp, clusterv1beta1.ControlPlaneComponentsHealthyCondition)
	}

	return ctrl.Result{}, nil
}

func (r *MicroK8sControlPlaneReconciler) reconcileMachines(ctx context.Context,
	cluster *clusterv1.Cluster, mcp *clusterv1beta1.MicroK8sControlPlane, machines []clusterv1.Machine) (res ctrl.Result, err error) {

	// If we've made it this far, we can assume that all ownedMachines are up to date
	numMachines := len(machines)
	desiredReplicas := int(*mcp.Spec.Replicas)

	controlPlane := r.newControlPlane(cluster, mcp, machines)

	switch {
	// We are creating the first replica
	case numMachines < desiredReplicas && numMachines == 0:
		// Create new Machine w/ init
		log.Info("initializing control plane", "Desired", desiredReplicas, "Existing", numMachines)

		return r.bootControlPlane(ctx, cluster, mcp, controlPlane, true)
	// We are scaling up
	case numMachines < desiredReplicas && numMachines > 0:
		conditions.MarkFalse(mcp, clusterv1beta1.ResizedCondition, clusterv1beta1.ScalingUpReason, clusterv1.ConditionSeverityWarning,
			"Scaling up control plane to %d replicas (actual %d)", desiredReplicas, numMachines)

		// Create a new Machine w/ join
		log.Info("scaling up control plane", "Desired", desiredReplicas, "Existing", numMachines)

		return r.bootControlPlane(ctx, cluster, mcp, controlPlane, false)
	// We are scaling down
	case numMachines > desiredReplicas:
		conditions.MarkFalse(mcp, clusterv1beta1.ResizedCondition, clusterv1beta1.ScalingDownReason, clusterv1.ConditionSeverityWarning,
			"Scaling down control plane to %d replicas (actual %d)",
			desiredReplicas, numMachines)

		if numMachines == 1 {
			conditions.MarkFalse(mcp, clusterv1beta1.ResizedCondition, clusterv1beta1.ScalingDownReason, clusterv1.ConditionSeverityError,
				"Cannot scale down control plane nodes to 0")

			return res, nil
		}

		if err := r.ensureNodesBooted(ctx, controlPlane.MCP, cluster, machines); err != nil {
			log.Info("waiting for all nodes to finish boot sequence", "error", err)

			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		if !conditions.IsTrue(mcp, clusterv1beta1.EtcdClusterHealthyCondition) {
			log.Info("waiting for etcd to become healthy before scaling down")

			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		log.Info("scaling down control plane", "Desired", desiredReplicas, "Existing", numMachines)

		res, err = r.scaleDownControlPlane(ctx, mcp, util.ObjectKey(cluster), controlPlane.MCP.Name, machines)
		if err != nil {
			if res.Requeue || res.RequeueAfter > 0 {
				log.Info("failed to scale down control plane", "error", err)

				return res, nil
			}
		}

		return res, err
	default:
		if !reflect.ValueOf(mcp.Spec.ControlPlaneConfig.InitConfig).IsZero() {
			mcp.Status.Bootstrapped = true
			conditions.MarkTrue(mcp, clusterv1beta1.MachinesBootstrapped)
		}

		if !mcp.Status.Bootstrapped {
			if err := r.bootstrapCluster(ctx, mcp, cluster, machines); err != nil {
				conditions.MarkFalse(mcp, clusterv1beta1.MachinesBootstrapped, clusterv1beta1.WaitingForMicroK8sBootReason, clusterv1.ConditionSeverityInfo, err.Error())

				log.Info("bootstrap failed, retrying in 20 seconds", "error", err)

				return ctrl.Result{RequeueAfter: time.Second * 20}, nil
			}

			conditions.MarkTrue(mcp, clusterv1beta1.MachinesBootstrapped)

			mcp.Status.Bootstrapped = true
		}

		if conditions.Has(mcp, clusterv1beta1.MachinesReadyCondition) {
			conditions.MarkTrue(mcp, clusterv1beta1.ResizedCondition)
		}

		conditions.MarkTrue(mcp, clusterv1beta1.MachinesCreatedCondition)
	}

	return ctrl.Result{}, nil
}

func (r *MicroK8sControlPlaneReconciler) reconcileExternalReference(ctx context.Context, ref corev1.ObjectReference, cluster *clusterv1.Cluster) error {
	obj, err := external.Get(ctx, r.Client, &ref, cluster.Namespace)
	if err != nil {
		return err
	}

	objPatchHelper, err := patch.NewHelper(obj, r.Client)
	if err != nil {
		return err
	}

	obj.SetOwnerReferences(util.EnsureOwnerRef(obj.GetOwnerReferences(), metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cluster.Name,
		UID:        cluster.UID,
	}))

	return objPatchHelper.Patch(ctx, obj)
}

func (r *MicroK8sControlPlaneReconciler) bootControlPlane(ctx context.Context, cluster *clusterv1.Cluster, mcp *clusterv1beta1.MicroK8sControlPlane,
	controlPlane *ControlPlane, first bool) (ctrl.Result, error) {
	// Since the cloned resource should eventually have a controller ref for the Machine, we create an
	// OwnerReference here without the Controller field set
	infraCloneOwner := &metav1.OwnerReference{
		APIVersion: clusterv1beta1.GroupVersion.String(),
		Kind:       "MicroK8sControlPlane",
		Name:       mcp.Name,
		UID:        mcp.UID,
	}

	// Clone the infrastructure template
	infraRef, err := external.CloneTemplate(ctx, &external.CloneTemplateInput{
		Client:      r.Client,
		TemplateRef: &mcp.Spec.InfrastructureTemplate,
		Namespace:   mcp.Namespace,
		OwnerRef:    infraCloneOwner,
		ClusterName: cluster.Name,
	})
	if err != nil {
		conditions.MarkFalse(mcp, clusterv1beta1.MachinesCreatedCondition,
			clusterv1beta1.InfrastructureTemplateCloningFailedReason,
			clusterv1.ConditionSeverityError, err.Error())

		return ctrl.Result{}, err
	}

	bootstrapConfig := &mcp.Spec.ControlPlaneConfig.ControlPlaneConfig
	if !reflect.ValueOf(mcp.Spec.ControlPlaneConfig.InitConfig).IsZero() && first {
		bootstrapConfig = &mcp.Spec.ControlPlaneConfig.InitConfig
	}

	// Clone the bootstrap configuration
	bootstrapRef, err := r.generateMicroK8sConfig(ctx, mcp, cluster, bootstrapConfig)
	if err != nil {
		conditions.MarkFalse(mcp, clusterv1beta1.MachinesCreatedCondition,
			clusterv1beta1.BootstrapTemplateCloningFailedReason,
			clusterv1.ConditionSeverityError, err.Error())

		return ctrl.Result{}, err
	}

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName(mcp.Name + "-"),
			Namespace: mcp.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterLabelName:             cluster.ClusterName,
				clusterv1.MachineControlPlaneLabelName: "",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(mcp, clusterv1beta1.GroupVersion.WithKind("MicroK8sControlPlane")),
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName:       cluster.Name,
			Version:           &mcp.Spec.Version,
			InfrastructureRef: *infraRef,
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: bootstrapRef,
			},
			//WARNING: This is a work around, I dont know how this is supposed to be set
		},
	}

	failureDomains := r.getFailureDomain(ctx, cluster)
	if len(failureDomains) > 0 {
		machine.Spec.FailureDomain = &failureDomains[rand.Intn(len(failureDomains))]
	}

	if err := r.Client.Create(ctx, machine); err != nil {
		conditions.MarkFalse(mcp, clusterv1beta1.MachinesCreatedCondition,
			clusterv1beta1.MachineGenerationFailedReason,
			clusterv1.ConditionSeverityError, err.Error())

		return ctrl.Result{}, errors.Wrap(err, "Failed to create machine")
	}

	return ctrl.Result{Requeue: true}, nil
}

func (r *MicroK8sControlPlaneReconciler) reconcileConditions(ctx context.Context, cluster *clusterv1.Cluster, tcp *clusterv1beta1.MicroK8sControlPlane,
	machines []clusterv1.Machine) (result ctrl.Result, err error) {
	if !conditions.Has(tcp, clusterv1beta1.AvailableCondition) {
		conditions.MarkFalse(tcp, clusterv1beta1.AvailableCondition, clusterv1beta1.WaitingForMicroK8sBootReason, clusterv1.ConditionSeverityInfo, "")
	}

	if !conditions.Has(tcp, clusterv1beta1.MachinesBootstrapped) {
		conditions.MarkFalse(tcp, clusterv1beta1.MachinesBootstrapped, clusterv1beta1.WaitingForMachinesReason, clusterv1.ConditionSeverityInfo, "")
	}

	return ctrl.Result{}, nil
}

// getFailureDomain will return a slice of failure domains from the cluster status.
func (r *MicroK8sControlPlaneReconciler) getFailureDomain(ctx context.Context, cluster *clusterv1.Cluster) []string {
	if cluster.Status.FailureDomains == nil {
		return nil
	}

	retList := []string{}
	for key := range cluster.Status.FailureDomains {
		retList = append(retList, key)
	}
	return retList
}

func (r *MicroK8sControlPlaneReconciler) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster,
	tcp *clusterv1beta1.MicroK8sControlPlane) (ctrl.Result, error) {
	// Get list of all control plane machines
	ownedMachines, err := r.getControlPlaneMachinesForCluster(ctx, util.ObjectKey(cluster), tcp.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	// If no control plane machines remain, remove the finalizer
	if len(ownedMachines) == 0 {
		controllerutil.RemoveFinalizer(tcp, clusterv1beta1.MicroK8sControlPlaneFinalizer)
		return ctrl.Result{}, r.Client.Update(ctx, tcp)
	}

	for _, ownedMachine := range ownedMachines {
		// Already deleting this machine
		if !ownedMachine.ObjectMeta.DeletionTimestamp.IsZero() {
			continue
		}
		// Submit deletion request
		if err := r.Client.Delete(ctx, &ownedMachine); err != nil && !apierrors.IsNotFound(err) {

			return ctrl.Result{}, err
		}
	}

	conditions.MarkFalse(tcp, clusterv1beta1.ResizedCondition, clusterv1.DeletingReason, clusterv1.ConditionSeverityInfo, "")
	// Requeue the deletion so we can check to make sure machines got cleaned up
	return ctrl.Result{RequeueAfter: requeueDuration}, nil
}

func (r *MicroK8sControlPlaneReconciler) bootstrapCluster(ctx context.Context, tcp *clusterv1beta1.MicroK8sControlPlane,
	cluster *clusterv1.Cluster, machines []clusterv1.Machine) error {

	addresses := []string{}
	for _, machine := range machines {
		found := false

		for _, addr := range machine.Status.Addresses {
			if addr.Type == clusterv1.MachineInternalIP {
				addresses = append(addresses, addr.Address)

				found = true

				break
			}
		}

		if !found {
			return fmt.Errorf("machine %q doesn't have an InternalIP address yet", machine.Name)
		}
	}

	if len(addresses) == 0 {
		return fmt.Errorf("no machine addresses to use for bootstrap")
	}

	return nil
}

func (r *MicroK8sControlPlaneReconciler) scaleDownControlPlane(ctx context.Context, tcp *clusterv1beta1.MicroK8sControlPlane,
	cluster client.ObjectKey, cpName string, machines []clusterv1.Machine) (ctrl.Result, error) {
	if len(machines) == 0 {
		return ctrl.Result{}, fmt.Errorf("no machines found")
	}

	log.Info("Found control plane machines", "machines", len(machines))

	kubeclient, err := r.kubeconfigForCluster(ctx, cluster)
	if err != nil {
		return ctrl.Result{RequeueAfter: 20 * time.Second}, err
	}

	defer kubeclient.Close() //nolint:errcheck

	deleteMachine := machines[0]
	for _, machine := range machines {
		if !machine.ObjectMeta.DeletionTimestamp.IsZero() {
			log.Info("machine is in process of deletion", "machine", machine.Name)

			node, err := kubeclient.CoreV1().Nodes().Get(ctx, machine.Status.NodeRef.Name, metav1.GetOptions{})
			if err != nil {
				// It's possible for the node to already be deleted in the workload cluster, so we just
				// requeue if that's that case instead of throwing a scary error.
				if apierrors.IsNotFound(err) {
					return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
				}
				return ctrl.Result{RequeueAfter: 20 * time.Second}, err
			}

			log.Info("Deleting node", "machine", machine.Name, "node", node.Name)

			err = kubeclient.CoreV1().Nodes().Delete(ctx, node.Name, metav1.DeleteOptions{})
			if err != nil {
				return ctrl.Result{RequeueAfter: 20 * time.Second}, err
			}

			return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
		}

		// do not allow scaling down until all nodes have nodeRefs
		if machine.Status.NodeRef == nil {
			log.Info("one of machines does not have NodeRef", "machine", machine.Name)

			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		if machine.CreationTimestamp.Before(&deleteMachine.CreationTimestamp) {
			deleteMachine = machine
		}
	}

	if deleteMachine.Status.NodeRef == nil {
		return ctrl.Result{RequeueAfter: 20 * time.Second}, fmt.Errorf("%q machine does not have a nodeRef", deleteMachine.Name)
	}

	node := deleteMachine.Status.NodeRef

	log.Info("deleting machine", "machine", deleteMachine.Name, "node", node.Name)

	err = r.Client.Delete(ctx, &deleteMachine)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("deleting node", "machine", deleteMachine.Name, "node", node.Name)

	err = kubeclient.CoreV1().Nodes().Delete(ctx, node.Name, metav1.DeleteOptions{})
	if err != nil {
		return ctrl.Result{RequeueAfter: 20 * time.Second}, err
	}

	// Requeue so that we handle any additional scaling.
	return ctrl.Result{Requeue: true}, nil
}
