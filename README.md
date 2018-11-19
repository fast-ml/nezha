# Nezha: Kubernetes Native Big Data Accelerator for Machine Learning

## Why?

ML/DL training performance using datasets stored at S3/GCS/Azure is subject to rate limiting and suboptimal downloading throughput.

Nezha automatically rewrites training jobs' Pod spec, reroutes S3/GCS/Azure requests to local cache, and thus accelerates overall performance and scalability.


## Mechanism

The Pod spec is modified by a mutating webhook. The webhook inspects deployments' or jobs' `app` label and applies host aliases to Pod spec.
Once the containers are up and running, S3/GCS/Azure requests are redirected to proxy's endpoint.

## Instruction

## Overview
To use Nezha, you just need to set the host aliases that Nezha acts as a reverse proxy, the Kubernetes Job or Deployment labels that Nezha's Webhook checks before injecting the hostaliases.

In the demo, the hostaliases configmap is as the follow. kubeflow creates Jobs and label them as `app.kubernetes.io/deploy-manager: ksonnet`. That is how the hostaliases configuration below set this label. 

``apiVersion: v1
kind: ConfigMap
metadata:
  name: hostaliases-config
data:
  config: |
      - name: dataset
        app: app.kubernetes.io/deploy-manager
        label: ksonnet
`yaml
```

The hostaliases are retrieved once the reverse proxy service is up. As seen in the [setup.sh scrtip](examples/demo/setup.sh), this is done via
```bash
kubectl get svc -n ${NAMESPACE} proxy-cache -o jsonpath={.spec.clusterIP}
```

Last, the remote servers that are proxy'ed are extracted from nginx configuration via
```bash
    SERVERS=$(grep server_name nginx.conf |tr -d ';' |awk '{print $2}')
```

Putting together, the `setup.sh` script does the following:
- create nginx service
- create signed certificates that are used by Webhook's TLS enabled HTTP server
- create Webhook
- create a configmap to store configurations for hostaliaes and expected Jobs or Deployments' labels 


## Setup Reverse Proxy Cache Service and Webhook

```bash
examples/demo/setup.sh start
```

You should expect the following service and pod running:

* Reverse Proxy Cache
```console
# kubectl get svc -n nezha-demo
NAME          TYPE        CLUSTER-IP    EXTERNAL-IP   PORT(S)   AGE
proxy-cache   ClusterIP   10.99.81.48   <none>        80/TCP    16m
# kubectl get pod -n nezha-demo
NAME                           READY   STATUS    RESTARTS   AGE
proxy-cache-655ff74648-bhg6r   1/1     Running   0          17m
```

* Webhook
```console
# kubectl get pod -l app=hostaliases-injector
 NAME                                                       READY   STATUS    RESTARTS   AGE
 hostaliases-injector-webhook-deployment-7b66fddb9d-b5xlg   1/1     Running   0          19m
```

## Run a test job that downloads MNIST dataset

```bash
kubectl apply -f examples/demo/kubeflow-test.yaml
```
The first time to run the test, the log from pod is like:

```console
# kubectl logs -n nezha-test nezha-job-test-284rb |tail
164700K .......... .......... .......... .......... .......... 99% 15.2M 0s
164750K .......... .......... .......... .......... .......... 99% 2.10M 0s
164800K .......... .......... .......... .......... .......... 99% 7.67M 0s
164850K .......... .......... .......... .......... .......... 99% 2.23M 0s
164900K .......... .......... .......... .......... .......... 99% 56.7M 0s
164950K .......... .......... .......... .......... .......... 99% 6.36M 0s
165000K .......... .......... .......... ..........           100% 1.89M=46s

2018-11-07 19:39:45 (3.49 MB/s) - 'cifar-100-python.tar.gz' saved [169001437/169001437]
```

Run the job again:
```bash
kubectl delete -f examples/demo/kubeflow-test.yaml
kubectl apply -f examples/demo/kubeflow-test.yaml
```

Then the file is cached and download is much faster:

```console
# kubectl logs -n nezha-test nezha-job-test-zs9bv |tail
164700K .......... .......... .......... .......... .......... 99%  449M 0s
164750K .......... .......... .......... .......... .......... 99%  371M 0s
164800K .......... .......... .......... .......... .......... 99%  396M 0s
164850K .......... .......... .......... .......... .......... 99%  448M 0s
164900K .......... .......... .......... .......... .......... 99%  445M 0s
164950K .......... .......... .......... .......... .......... 99%  456M 0s
165000K .......... .......... .......... ..........           100%  366M=0.5s

2018-11-07 19:41:15 (323 MB/s) - 'cifar-100-python.tar.gz' saved [169001437/169001437]
```

## Clean up Reverse Proxy Cache Service and Webhook

```bash
examples/demo/setup.sh clean
```

## Acknowledgement

Some initial implementation of initializer is based on https://github.com/kelseyhightower/kubernetes-initializer-tutorial
Some initial webhook implementation is based on Kubernetes e2e tests.