cat >> cluster.config << EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
    - hostPath: /var/run/docker.sock
      containerPath: /var/run/docker.sock
EOF
kind create cluster --config cluster.config
clusterctl init --infrastructure docker
kubectl delete namespace capi-kubeadm-bootstrap-system
kubectl delete namespace capi-kubeadm-control-plane-system
make install
cd ../cluster-api-bootstrap-provider-microk8s
make install
cd ../cluster-api-control-plane-provider-microk8s