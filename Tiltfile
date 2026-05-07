# KubeChan — Local dev with Tilt
# Prerequisites: Docker Desktop Kubernetes enabled
# Usage: tilt up

# Create namespace and install CRDs first (they must exist before Helm rendering)
local_resource(
    'kubechan-namespace',
    cmd='kubectl create namespace kubechan --dry-run=client -o yaml | kubectl apply -f -',
    labels=['setup'],
)

k8s_resource(
    objects=[
        'problemcases.kubechan.io:CustomResourceDefinition:kubechan',
        'diagnosticruns.kubechan.io:CustomResourceDefinition:kubechan',
        'incidents.kubechan.io:CustomResourceDefinition:kubechan',
        'kubechanstates.kubechan.io:CustomResourceDefinition:kubechan',
        'kubechanexclusionrules.kubechan.io:CustomResourceDefinition:kubechan',
    ],
    new_name='kubechan-crds',
    resource_deps=['kubechan-namespace'],
    labels=['setup'],
)

# Go services use the repo root as build context (shared go.mod + api/ package)
docker_build(
    'kubechan/cluster-watcher',
    '.',
    dockerfile='services/cluster-watcher/Dockerfile',
    only=['go.mod', 'go.sum', 'api/', 'services/cluster-watcher/'],
    live_update=[
        sync('api/', '/workspace/api/'),
        sync('services/cluster-watcher/', '/workspace/services/cluster-watcher/'),
        run('CGO_ENABLED=0 go build -o /cluster-watcher ./services/cluster-watcher/'),
    ],
)

docker_build(
    'kubechan/diagnostics-worker',
    '.',
    dockerfile='services/diagnostics-worker/Dockerfile',
    only=['go.mod', 'go.sum', 'api/', 'services/diagnostics-worker/'],
    live_update=[
        sync('api/', '/workspace/api/'),
        sync('services/diagnostics-worker/', '/workspace/services/diagnostics-worker/'),
        run('CGO_ENABLED=0 go build -o /diagnostics-worker ./services/diagnostics-worker/'),
    ],
)

docker_build(
    'kubechan/backend-api',
    '.',
    dockerfile='services/backend-api/Dockerfile',
    only=['go.mod', 'go.sum', 'api/', 'services/backend-api/'],
    live_update=[
        sync('api/', '/workspace/api/'),
        sync('services/backend-api/', '/workspace/services/backend-api/'),
        run('CGO_ENABLED=0 go build -o /backend-api ./services/backend-api/'),
    ],
)

docker_build(
    'kubechan/llm-gateway',
    'services/llm-gateway',
    live_update=[
        sync('services/llm-gateway/', '/app/'),
        run('pip install -r /app/requirements.txt', trigger=['services/llm-gateway/requirements.txt']),
    ],
)

docker_build(
    'kubechan/frontend-ui',
    'services/frontend-ui',
    live_update=[
        sync('services/frontend-ui/src/', '/app/src/'),
    ],
)

# Deploy via Helm with dev overrides
k8s_yaml(helm(
    'helm/kubechan',
    name='kubechan',
    namespace='kubechan',
    values=['helm/kubechan/values-dev.yaml'],
))

# Ensure CRDs exist before the Helm resources are applied
for svc in ['kubechan-cluster-watcher', 'kubechan-diagnostics-worker', 'kubechan-backend-api', 'kubechan-llm-gateway', 'kubechan-frontend-ui']:
    k8s_resource(svc, resource_deps=['kubechan-crds', 'kubechan-namespace'])

