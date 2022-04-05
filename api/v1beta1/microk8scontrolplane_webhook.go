package v1beta1

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *MicroK8sControlPlane) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}
