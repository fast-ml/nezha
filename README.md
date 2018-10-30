# Nezha: Kubernetes Native Big Data Accelerator for Machine Learning

## Why?

ML/DL training performance using datasets stored at S3/GCS/Azure is subject to rate limiting and suboptimal downloading throughput.

Nezha automatically rewrites training jobs' Pod spec, reroutes S3/GCS/Azure requests to local cache, and thus accelerates overall performance and scalability.


## Mechanism

The Pod spec is modified by a mutating webhook. The webhook inspects deployments' or jobs' `app` label and applies host aliases to Pod spec.
Once the containers are up and running, S3/GCS/Azure requests are redirected to proxy's endpoint.


## Acknowledgement

Some initial implementation of initializer is based on https://github.com/kelseyhightower/kubernetes-initializer-tutorial
Some initial webhook implementation is based on Kubernetes e2e tests.