# cat >> cluster.config << EOF
# kind: Cluster
# apiVersion: kind.x-k8s.io/v1beta1
# nodes:
# - role: control-plane
#   extraMounts:
#     - hostPath: /var/run/docker.sock
#       containerPath: /var/run/docker.sock
# EOF
export AWS_B64ENCODED_CREDENTIALS=$(clusterawsadm bootstrap credentials encode-as-profile)
export AWS_REGION=eu-west-1
export AWS_CONTROL_PLANE_MACHINE_TYPE=t3.medium
export AWS_NODE_MACHINE_TYPE=t3.medium
export AWS_SSH_KEY_NAME=default
kind create cluster
#kind create cluster --config cluster.config
clusterctl init --infrastructure aws --bootstrap "-" --control-plane "-"
kubectl delete namespace capi-kubeadm-bootstrap-system
kubectl delete namespace capi-kubeadm-control-plane-system
make install
cd ../cluster-api-bootstrap-provider-microk8s
make install
cd ../cluster-api-control-plane-provider-microk8s
