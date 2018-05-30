# Nezha: Kubernetes Native Big Data Accelerator for Machine Learning

## Why?

ML training performance using datasets stored at S3 is subject to S3's rate limiting and suboptimal downloading throughput.

Nezha automatically rewrites ML Pod spec, reroutes S3 requests to local cache, and thus accelerates overall performance and scalability.


## Mechanism

The Pod spec is modified by a dynamic webhook initializer. The initializer inspects Pod's `app` label to apply S3 caching sidecar.


## Acknowledgement

Some initial implementation of dynamic webhook is based on https://github.com/kelseyhightower/kubernetes-initializer-tutorial