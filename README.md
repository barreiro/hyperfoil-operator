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

## Deploy the operator and run Hyperfoil in minikube
        
First start by [installing minikube](https://minikube.sigs.k8s.io/docs/start/). Start the cluster with `minikube start`. (*Suggestion:* use `minikube dashboard` to monitor the cluster)
                       
Build and install Hyperfoil operator (Go 1.19 is required) with `make build install`.

Deploy the example resource in [config/samples/_v1alpha2_hyperfoil.yaml](config/samples/_v1alpha2_hyperfoil.yaml) in the cluster with `make deploy-samples`

Run the operator with `make run`. Once the hyperfoil controller has started the operator can be stopped, as it only reacts to changes. 

To expose the controller to the host network, need to forward port 8090 with `kubectl port-forward --address 0.0.0.0 service/hyperfoil 8090:8090`

You can connect to the Hyperfoil controller using the hyperfoil-cli or the [web-cli](http://127.0.0.1:8090)
                                           
From here on, follow the [Hyperfoil quickstart guide](https://hyperfoil.io/quickstart) for instructions on how to deploy and run a benchmark.

### Undeploy Hyperfoil

Undeploy samples from cluster `make undeploy-samples` (*Note:* this does not require the operator to be running)

Stop the minikube cluster with `minikube stop`. Optionally delete the cluster with `minikube delete --all`
