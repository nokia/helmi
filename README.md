# helmi
Open Service Broker API Implementation using helm

![alt Logo](docs/logo.png)

## Start locally

```console
# start minikube
minikube start

# init helm and install tiller (needed once)
helm init

# build helmi
go get -d github.com/monostream/helmi
cd ${GOPATH:-~/go}/src/github.com/monostream/helmi
go build

# run helmi
./helmi
```

## Start on kubernetes

```console
# create serviceaccount, clusterrolebinding, deployment, service and an optional secret for basic authorization
kubectl create -f docs/kubernetes/kube-helmi-rbac.yaml
kubectl create -f docs/kube-helmi-secret.yaml
kubectl create -f docs/kubernetes/kube-helmi.yaml

# curl to catalog with basic auth
curl --user {username}:{password} http://$(kubernetes ip):30000/v2/catalog
```
or
```console
./docs/kubernetes/deploy.sh

# curl to catalog with basic auth
curl --user {username}:{password} http://$(kubernetes ip):30000/v2/catalog
```

## Use in Cloud Foundry

Register Helmi Service Broker

```console
cf create-service-broker helmi {username} {password} http://{IP}:5000
```

List and allow service access

```console
cf service-access
cf enable-service-access {service}
```

List marketplace and create service instance

```console
cf marketplace
cf create-service {service} {plan} {name}
```

Bind service to application

```console
cf bind-service {app} {name}
```

## Tests
run tests
```console
go test ./pkg/* -v
```

## Environment Variables

Helmi can use environment variables to define a dns name for connection strings and a username/password for basic authentication.

To use basic authentication set `USERNAME` and `PASSWORD` environment variables. In the k8s deployment they are read from a secret, see [kube-helmi-secret.yaml](docs/kubernetes/kube-helmi-secret.yaml)

To replace the connection string IPs set an environment variable `DOMAIN`.
