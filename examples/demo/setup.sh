#!/bin/bash
set -e

NAMESPACE=${NAMESPACE:-"nezha-demo"}

start() {
    kubectl create ns ${NAMESPACE} || true
    # create nginx config as a configmap
    kubectl create -n ${NAMESPACE} configmap nginx-proxy --from-file=nginx.conf || true
    # create nginx and svc
    kubectl apply -n ${NAMESPACE} -f nginx.yaml
    # create csr
    ../../deploy/create-signed-crt.sh
    # create webhook svc
    CA_BUNDLE=$(kubectl get configmap -n kube-system extension-apiserver-authentication -o=jsonpath='{.data.client-ca-file}' | base64 | tr -d '\n')
    cat ../../deploy/mutatingwebhook.yaml | sed -e "s|\${CA_BUNDLE}|${CA_BUNDLE}|g" | kubectl apply -f -
    # patch host aliases
    SVC=$(kubectl get svc -n ${NAMESPACE} proxy-cache -o jsonpath={.spec.clusterIP})    
    SERVERS=$(grep server_name nginx.conf |tr -d ';' |awk '{print $2}')
    file=$(mktemp temp.XXX.yaml)
    cat > ${file} <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: hostaliases-config
data:
  config: |
      - name: dataset
        app: app.kubernetes.io/deploy-manager
        label: ksonnet
        hostAliases:
        - ip: "${SVC}"
          hostnames:
EOF
    for s in ${SERVERS}
    do
        echo "          - \"${s}\"" >> ${file}
    done    
    kubectl apply -f ${file}
    rm ${file}
}

clean() {
    kubectl delete -f ../../deploy/mutatingwebhook.yaml
    kubectl delete  -n ${NAMESPACE} -f nginx.yaml
    kubectl delete  -n ${NAMESPACE} configmap nginx-proxy
    kubectl delete ns ${NAMESPACE}
    exit 0
}


usage() {
        cat <<EOF
usage: ${0} start|clean
EOF
        exit 1
}

while [[ $# -gt 0 ]]
do
    case ${1} in
        start)
            start
            ;;
        clean)
            clean
            ;;
        *)
            usage
            ;;
    esac
    shift
done

    
