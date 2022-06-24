# Hyperfoil Operator

This operator installs and configures Hyperfoil Controller. See example resource in [config/samples/_v1alpha2_hyperfoil.yaml](config/samples/_v1alpha2_hyperfoil.yaml).

## Building the operator

```bash
# Define your container repository.
export CONTAINER_REPO_OVERRIDE="quay.io/hyperfoil" # default: quay.io/hyperfoil
# This creates and pushes the image (${CONTAINER_REPO_OVERRIDE}/hyperfoil-operator:0.15.0)
# Build is executed in a builder container.
make docker-build docker-push
# Creates image (${CONTAINER_REPO_OVERRIDE}/hyperfoil-operator-bundle:0.15.0) with ClusterServiceVersion and other resources
make bundle-build
podman push ${CONTAINER_REPO_OVERRIDE}/hyperfoil-operator-bundle:0.15.0
# Creates CatalogSource and Subscription in the cluster
operator-sdk run bundle ${CONTAINER_REPO_OVERRIDE}/hyperfoil-operator-bundle:0.15.0
```