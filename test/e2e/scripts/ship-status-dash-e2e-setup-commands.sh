#!/bin/bash

set -e

export ARTIFACT_DIR="${ARTIFACT_DIR:=/tmp/ship_status_artifacts}"
mkdir -p $ARTIFACT_DIR

export NAMESPACE="ship-status-e2e"
export E2E_NS="${NAMESPACE}"
DB_NAME="ship_status_test"
DSN="postgres://postgres:testpass@postgres.${NAMESPACE}.svc.cluster.local:5432/${DB_NAME}?sslmode=disable&client_encoding=UTF8"

echo "The dashboard CI image: ${DASHBOARD_IMAGE}"
echo "The mock-oauth-proxy CI image: ${MOCK_OAUTH_PROXY_IMAGE}"
echo "The migrate CI image: ${MIGRATE_IMAGE}"
echo "The seed-slo CI image: ${SEED_SLO_IMAGE}"
echo "The component-monitor CI image: ${COMPONENT_MONITOR_IMAGE}"
echo "The mock-monitored-component CI image: ${MOCK_MONITORED_COMPONENT_IMAGE}"
KUBECTL_CMD="${KUBECTL_CMD:=oc}"
export KUBECTL_CMD
echo "The kubectl command is: ${KUBECTL_CMD}"

is_ready=0
echo "Waiting for cluster to be usable..."

e2e_pause() {
  if [ -z $OPENSHIFT_CI ]; then
    return
  fi
  echo "Sleeping 30 seconds ..."
  sleep 30
}

function download_envsubst() {
  mkdir -p /tmp/bin
  export PATH=/tmp/bin:$PATH
  curl -L https://github.com/a8m/envsubst/releases/download/v1.4.2/envsubst-Linux-x86_64 -o /tmp/bin/envsubst && chmod +x /tmp/bin/envsubst
}

set +e
for i in `seq 1 20`; do
  echo -n "${i})"
  e2e_pause
  echo "Checking cluster nodes"
  ${KUBECTL_CMD} get node
  if [ $? -eq 0 ]; then
    echo "Cluster looks ready"
    is_ready=1
    break
  fi
  echo "Cluster not ready yet..."
done
set -e

echo "KUBECONFIG=${KUBECONFIG}"
echo "Showing kube context"
${KUBECTL_CMD} config current-context

if [ $is_ready -eq 0 ]; then
  echo "Cluster never became ready aborting"
  exit 1
fi

echo "Creating namespace..."
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
  labels:
    openshift.io/run-level: "0"
    openshift.io/cluster-monitoring: "true"
    pod-security.kubernetes.io/enforce: privileged
    pod-security.kubernetes.io/audit: privileged
    pod-security.kubernetes.io/warn: privileged
END

echo "Starting postgres..."
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: postgres
  namespace: ${NAMESPACE}
  labels:
    app: postgres
spec:
  volumes:
    - name: postgresdb
      emptyDir: {}
  containers:
  - name: postgres
    image: quay.io/enterprisedb/postgresql:latest
    ports:
    - containerPort: 5432
    env:
    - name: POSTGRES_PASSWORD
      value: testpass
    volumeMounts:
      - mountPath: /var/lib/postgresql/data
        name: postgresdb
    securityContext:
      privileged: false
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
      runAsNonRoot: true
      runAsUser: 3
      seccompProfile:
        type: RuntimeDefault
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: postgres
  name: postgres
  namespace: ${NAMESPACE}
spec:
  ports:
  - name: postgres
    port: 5432
    protocol: TCP
  selector:
    app: postgres
END

echo "Waiting for postgres pod to be Ready ..."
set +e
TIMEOUT=120s
${KUBECTL_CMD} -n ${NAMESPACE} wait --for=condition=Ready pod/postgres --timeout=${TIMEOUT}
postgres_retVal=$?
set -e

${KUBECTL_CMD} -n ${NAMESPACE} logs postgres > ${ARTIFACT_DIR}/postgres.log || true
if [ ${postgres_retVal} -ne 0 ]; then
  echo "Postgres pod never came up"
  exit 1
fi

e2e_pause # This pause is important to give the postgres pod time to start up and be ready to accept connections.

${KUBECTL_CMD} -n ${NAMESPACE} get po -o wide
${KUBECTL_CMD} -n ${NAMESPACE} get svc,ep

echo "Creating ${DB_NAME} database..."
${KUBECTL_CMD} -n ${NAMESPACE} exec postgres -- psql -U postgres -c "CREATE DATABASE ${DB_NAME};" || echo "Database might already exist"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
echo "SCRIPT_DIR: ${SCRIPT_DIR}"
echo "TEST_DIR: ${TEST_DIR}"
cd "$TEST_DIR"

CONFIG_FILE="${SCRIPT_DIR}/dashboard-config.yaml"
MOCK_OAUTH_PROXY_CONFIG_FILE="${SCRIPT_DIR}/mock-oauth-proxy-config.yaml"
COMPONENT_MONITOR_CONFIG_FILE="${SCRIPT_DIR}/component-monitor-config.yaml"

echo "Creating configmaps and secrets..."
export TEST_DASHBOARD_CONFIG_PATH="dashboard-config"
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${TEST_DASHBOARD_CONFIG_PATH}
  namespace: ${NAMESPACE}
data:
  config.yaml: |
$(sed 's/^/    /' "${CONFIG_FILE}")
END

cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: mock-oauth-proxy-config
  namespace: ${NAMESPACE}
data:
  config.yaml: |
$(sed 's/^/    /' "${MOCK_OAUTH_PROXY_CONFIG_FILE}")
END

export TEST_MOCK_MONITORED_COMPONENT_URL="http://mock-monitored-component.${NAMESPACE}.svc.cluster.local:9000"
export TEST_PROMETHEUS_URL="http://prometheus.${NAMESPACE}.svc.cluster.local:9090"
export TEST_COMPONENT_MONITOR_CONFIG_PATH="component-monitor-config"

# Ensure envsubst is available
if ! command -v envsubst &> /dev/null; then
  echo "envsubst not found, downloading..."
  download_envsubst
fi

cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${TEST_COMPONENT_MONITOR_CONFIG_PATH}
  namespace: ${NAMESPACE}
data:
  config.yaml: |
$(envsubst '$TEST_MOCK_MONITORED_COMPONENT_URL $TEST_PROMETHEUS_URL' < "${COMPONENT_MONITOR_CONFIG_FILE}" | sed 's/^/    /')
END

echo "Component-monitor ConfigMap created. Contents:"
${KUBECTL_CMD} -n ${NAMESPACE} get configmap ${TEST_COMPONENT_MONITOR_CONFIG_PATH} -o jsonpath='{.data.config\.yaml}' | cat
echo ""
echo "--- End of component-monitor config ---"

HMAC_SECRET=$(openssl rand -hex 32)
${KUBECTL_CMD} -n ${NAMESPACE} create secret generic hmac-secret --from-literal=secret="${HMAC_SECRET}" --dry-run=client -o yaml | ${KUBECTL_CMD} apply -f -

${KUBECTL_CMD} -n ${NAMESPACE} create secret generic regcred --from-file=.dockerconfigjson=${DOCKERCONFIGJSON} --type=kubernetes.io/dockerconfigjson --dry-run=client -o yaml | ${KUBECTL_CMD} apply -f -

echo "Creating hardcoded service account token secret..."
${KUBECTL_CMD} -n ${NAMESPACE} create secret generic component-monitor-token --from-literal=token="component-monitor-sa-token" --dry-run=client -o yaml | ${KUBECTL_CMD} apply -f -

echo "Running database migration..."
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: migrate-db
  namespace: ${NAMESPACE}
spec:
  template:
    spec:
      containers:
      - name: migrate
        image: ${MIGRATE_IMAGE}
        imagePullPolicy: Always
        command: ["./migrate"]
        args:
          - "--dsn=${DSN}"
      imagePullSecrets:
      - name: regcred
      restartPolicy: Never
  backoffLimit: 3
END

set +e
${KUBECTL_CMD} -n ${NAMESPACE} wait --for=condition=complete job/migrate-db --timeout=120s
migrate_retVal=$?
set -e

job_pod=$(${KUBECTL_CMD} -n ${NAMESPACE} get pod --selector=job-name=migrate-db --output=jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ ! -z "$job_pod" ]; then
  ${KUBECTL_CMD} -n ${NAMESPACE} logs ${job_pod} > ${ARTIFACT_DIR}/migrate.log || true
fi

if [ ${migrate_retVal} -ne 0 ]; then
  echo "Migration failed"
  exit 1
fi

echo "Seeding SLO payloads..."
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: seed-slo
  namespace: ${NAMESPACE}
spec:
  template:
    spec:
      containers:
      - name: seed-slo
        image: ${SEED_SLO_IMAGE}
        imagePullPolicy: Always
        command: ["./seed-slo"]
        args:
          - "--dsn=${DSN}"
          - "--config=/etc/config/config.yaml"
        volumeMounts:
        - mountPath: /etc/config
          name: dashboard-config
          readOnly: true
      imagePullSecrets:
      - name: regcred
      volumes:
      - name: dashboard-config
        configMap:
          name: ${TEST_DASHBOARD_CONFIG_PATH}
      restartPolicy: Never
  backoffLimit: 3
END

set +e
${KUBECTL_CMD} -n ${NAMESPACE} wait --for=condition=complete job/seed-slo --timeout=120s
seed_retVal=$?
set -e

seed_pod=$(${KUBECTL_CMD} -n ${NAMESPACE} get pod --selector=job-name=seed-slo --output=jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ ! -z "$seed_pod" ]; then
  ${KUBECTL_CMD} -n ${NAMESPACE} logs ${seed_pod} > ${ARTIFACT_DIR}/seed-slo.log || true
fi

if [ ${seed_retVal} -ne 0 ]; then
  echo "SLO seed failed"
  exit 1
fi

echo "Starting dashboard..."
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: dashboard
  namespace: ${NAMESPACE}
  labels:
    app: dashboard
spec:
  containers:
  - name: dashboard
    image: ${DASHBOARD_IMAGE}
    imagePullPolicy: Always
    ports:
    - containerPort: 8080
    command: ["./dashboard"]
    args:
      - "--config=/etc/config/config.yaml"
      - "--port=8080"
      - "--dsn=${DSN}"
      - "--hmac-secret-file=/etc/hmac/secret"
      - "--absent-report-check-interval=15s"
      - "--trt-payload-prune-interval=15s"
      - "--config-update-poll-interval=10s"
      - "--slack-base-url=http://localhost:3030"
      - "--slack-workspace-url=https://rhsandbox.slack.com/"
    volumeMounts:
    - mountPath: /etc/config
      name: dashboard-config
      readOnly: true
    - mountPath: /etc/hmac
      name: hmac-secret
      readOnly: true
  imagePullSecrets:
  - name: regcred
  volumes:
    - name: dashboard-config
      configMap:
        name: ${TEST_DASHBOARD_CONFIG_PATH}
    - name: hmac-secret
      secret:
        secretName: hmac-secret
  securityContext:
    privileged: false
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    runAsNonRoot: true
    runAsUser: 1001
    seccompProfile:
      type: RuntimeDefault
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: dashboard
  name: dashboard
  namespace: ${NAMESPACE}
spec:
  ports:
  - name: http
    port: 8080
    protocol: TCP
  selector:
    app: dashboard
END

echo "Starting mock-oauth-proxy..."
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: mock-oauth-proxy
  namespace: ${NAMESPACE}
  labels:
    app: mock-oauth-proxy
spec:
  containers:
  - name: mock-oauth-proxy
    image: ${MOCK_OAUTH_PROXY_IMAGE}
    imagePullPolicy: Always
    ports:
    - containerPort: 8443
    command: ["./mock-oauth-proxy"]
    args:
      - "--config=/etc/config/config.yaml"
      - "--port=8443"
      - "--upstream=http://dashboard.${NAMESPACE}.svc.cluster.local:8080"
      - "--hmac-secret-file=/etc/hmac/secret"
    volumeMounts:
    - mountPath: /etc/config
      name: mock-oauth-proxy-config
      readOnly: true
    - mountPath: /etc/hmac
      name: hmac-secret
      readOnly: true
  imagePullSecrets:
  - name: regcred
  volumes:
    - name: mock-oauth-proxy-config
      configMap:
        name: mock-oauth-proxy-config
    - name: hmac-secret
      secret:
        secretName: hmac-secret
  securityContext:
    privileged: false
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    runAsNonRoot: true
    runAsUser: 1001
    seccompProfile:
      type: RuntimeDefault
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: mock-oauth-proxy
  name: mock-oauth-proxy
  namespace: ${NAMESPACE}
spec:
  ports:
  - name: http
    port: 8443
    protocol: TCP
  selector:
    app: mock-oauth-proxy
END

echo "Starting mock-monitored-component..."
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: mock-monitored-component
  namespace: ${NAMESPACE}
  labels:
    app: mock-monitored-component
spec:
  containers:
  - name: mock-monitored-component
    image: ${MOCK_MONITORED_COMPONENT_IMAGE}
    imagePullPolicy: Always
    ports:
    - containerPort: 9000
    command: ["./mock-monitored-component"]
    args:
      - "--port=9000"
  imagePullSecrets:
  - name: regcred
  securityContext:
    privileged: false
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    runAsNonRoot: true
    runAsUser: 1001
    seccompProfile:
      type: RuntimeDefault
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: mock-monitored-component
  name: mock-monitored-component
  namespace: ${NAMESPACE}
spec:
  ports:
  - name: http
    port: 9000
    protocol: TCP
  selector:
    app: mock-monitored-component
END

echo "Starting Prometheus..."
PROMETHEUS_CONFIG_FILE="${SCRIPT_DIR}/prometheus.yml"
export MOCK_MONITORED_COMPONENT_TARGET="mock-monitored-component.${NAMESPACE}.svc.cluster.local:9000"
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-config
  namespace: ${NAMESPACE}
data:
  prometheus.yml: |
$(envsubst '$MOCK_MONITORED_COMPONENT_TARGET' < "${PROMETHEUS_CONFIG_FILE}" | sed 's/^/    /')
END

cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: prometheus
  namespace: ${NAMESPACE}
  labels:
    app: prometheus
spec:
  containers:
  - name: prometheus
    image: quay.io/prometheus/prometheus:latest
    imagePullPolicy: Always
    ports:
    - containerPort: 9090
    args:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.path=/prometheus"
      - "--web.console.libraries=/usr/share/prometheus/console_libraries"
      - "--web.console.templates=/usr/share/prometheus/consoles"
      - "--web.enable-lifecycle"
    volumeMounts:
    - mountPath: /etc/prometheus
      name: prometheus-config
      readOnly: true
  volumes:
    - name: prometheus-config
      configMap:
        name: prometheus-config
  securityContext:
    privileged: false
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    runAsNonRoot: true
    runAsUser: 65534
    seccompProfile:
      type: RuntimeDefault
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: prometheus
  name: prometheus
  namespace: ${NAMESPACE}
spec:
  ports:
  - name: http
    port: 9090
    protocol: TCP
  selector:
    app: prometheus
END

echo "Waiting for Prometheus pod to be Ready..."
set +e
TIMEOUT=60s
${KUBECTL_CMD} -n ${NAMESPACE} wait --for=condition=Ready pod/prometheus --timeout=${TIMEOUT}
prometheus_retVal=$?
set -e

if [ ${prometheus_retVal} -ne 0 ]; then
  echo "Prometheus pod never came up"
  ${KUBECTL_CMD} -n ${NAMESPACE} logs prometheus > ${ARTIFACT_DIR}/prometheus.log || true
  exit 1
fi

echo "Waiting for Prometheus to complete initial scrape..."
for i in `seq 1 60`; do
  if ${KUBECTL_CMD} -n ${NAMESPACE} exec prometheus -- wget -q -O- "http://localhost:9090/api/v1/query?query=mock_monitored_component_initialized" 2>/dev/null | grep -q "mock_monitored_component_initialized"; then
    echo "Prometheus has completed initial scrape"
    break
  fi
  if [ $i -eq 60 ]; then
    echo "Prometheus failed to complete initial scrape within 60 seconds"
    ${KUBECTL_CMD} -n ${NAMESPACE} logs prometheus > ${ARTIFACT_DIR}/prometheus-scrape.log || true
    exit 1
  fi
  sleep 1
done

echo "Starting component-monitor..."
cat << END | ${KUBECTL_CMD} apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: component-monitor
  namespace: ${NAMESPACE}
  labels:
    app: component-monitor
spec:
  containers:
  - name: component-monitor
    image: ${COMPONENT_MONITOR_IMAGE}
    imagePullPolicy: Always
    command: ["./component-monitor"]
    args:
      - "--config-path=/etc/config/config.yaml"
      - "--dashboard-url=http://mock-oauth-proxy.${NAMESPACE}.svc.cluster.local:8443"
      - "--name=e2e-component-monitor"
      - "--config-update-poll-interval=10s"
      - "--report-auth-token-file=/etc/token/token"
    readinessProbe:
      httpGet:
        path: /health
        port: 8080
      periodSeconds: 2
      failureThreshold: 15
    volumeMounts:
    - mountPath: /etc/config
      name: component-monitor-config
      readOnly: true
    - mountPath: /etc/token
      name: component-monitor-token
      readOnly: true
  imagePullSecrets:
  - name: regcred
  volumes:
    - name: component-monitor-config
      configMap:
        name: ${TEST_COMPONENT_MONITOR_CONFIG_PATH}
    - name: component-monitor-token
      secret:
        secretName: component-monitor-token
  securityContext:
    privileged: false
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    runAsNonRoot: true
    runAsUser: 1001
    seccompProfile:
      type: RuntimeDefault
END

echo "Waiting for dashboard, mock-oauth-proxy, mock-monitored-component, and component-monitor pods to be Ready ..."
set +e
TIMEOUT=60s
${KUBECTL_CMD} -n ${NAMESPACE} wait --for=condition=Ready pod/dashboard --timeout=${TIMEOUT}
dashboard_retVal=$?
${KUBECTL_CMD} -n ${NAMESPACE} wait --for=condition=Ready pod/mock-oauth-proxy --timeout=${TIMEOUT}
proxy_retVal=$?
${KUBECTL_CMD} -n ${NAMESPACE} wait --for=condition=Ready pod/mock-monitored-component --timeout=${TIMEOUT}
mock_component_retVal=$?
${KUBECTL_CMD} -n ${NAMESPACE} wait --for=condition=Ready pod/component-monitor --timeout=${TIMEOUT}
component_monitor_retVal=$?
set -e

if [ ${dashboard_retVal} -ne 0 ] || [ ${proxy_retVal} -ne 0 ] || [ ${mock_component_retVal} -ne 0 ] || [ ${component_monitor_retVal} -ne 0 ]; then
  echo "Pod startup failed, debugging..."
  ${KUBECTL_CMD} -n ${NAMESPACE} describe pod dashboard
  ${KUBECTL_CMD} -n ${NAMESPACE} describe pod mock-oauth-proxy
  ${KUBECTL_CMD} -n ${NAMESPACE} describe pod mock-monitored-component
  ${KUBECTL_CMD} -n ${NAMESPACE} describe pod component-monitor
fi

${KUBECTL_CMD} -n ${NAMESPACE} logs dashboard > ${ARTIFACT_DIR}/dashboard.log || true
${KUBECTL_CMD} -n ${NAMESPACE} logs mock-oauth-proxy > ${ARTIFACT_DIR}/mock-oauth-proxy.log || true
${KUBECTL_CMD} -n ${NAMESPACE} logs mock-monitored-component > ${ARTIFACT_DIR}/mock-monitored-component.log || true
${KUBECTL_CMD} -n ${NAMESPACE} logs prometheus > ${ARTIFACT_DIR}/prometheus.log || true
${KUBECTL_CMD} -n ${NAMESPACE} logs component-monitor > ${ARTIFACT_DIR}/component-monitor.log || true

if [ ${dashboard_retVal} -ne 0 ]; then
  echo "Dashboard pod never came up"
  exit 1
fi
if [ ${proxy_retVal} -ne 0 ]; then
  echo "Mock oauth-proxy pod never came up"
  exit 1
fi
if [ ${mock_component_retVal} -ne 0 ]; then
  echo "Mock-monitored-component pod never came up"
  exit 1
fi
if [ ${component_monitor_retVal} -ne 0 ]; then
  echo "Component-monitor pod never came up"
  exit 1
fi

sleep 30

${KUBECTL_CMD} -n ${NAMESPACE} get po -o wide
${KUBECTL_CMD} -n ${NAMESPACE} get svc,ep

date
