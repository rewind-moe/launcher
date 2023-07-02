# launcher

HTTP API to for launching Kubernetes Jobs.

## How to use

Run the service

```sh
go build
./launhcer -job-spec ./example/job-spec.yaml -kubeconfig ~/.kube/config
```

Create a job

```sh
curl -XPUT /api/v1/live/InsertVideoIdHere
```

Launhcer will then deploy the Job specified in `job-spec.yaml`. The yaml
contents can be templated to invoke a custom command that takes in the video ID
as input, e.g. via command line parameters.

The `-kubeconfig` flag is optional. Launcher will read the in-cluster config if
it's running inside of a Kubernetes cluster.

## Testing

Requirements

- minikube with docker

```
go test -v -run ^TestEndToEnd$ github.com/rewind-moe/launcher/tests
```
