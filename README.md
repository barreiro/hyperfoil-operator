# Hyperfoil Operator

This operator installs and configures Hyperfoil Controller. See example resource in [config/samples/_v1alpha2_hyperfoil.yaml](config/samples/_v1alpha2_hyperfoil.yaml).

## Building the operator

```bash
# This creates and pushes the image (quay.io/hyperfoil/hyperfoil-operator:0.15.0)
# Build is executed in a builder container.
make docker-build docker-push
# Creates image (quay.io/hyperfoil/hyperfoil-operator-bundle:0.15.0) with ClusterServiceVersion and other resources
make bundle-build
podman push quay.io/hyperfoil/hyperfoil-operator-bundle:0.15.0
# Creates CatalogSource and Subscription in the cluster
operator-sdk run bundle quay.io/hyperfoil/hyperfoil-operator-bundle:0.15.0
```