# KubeChan — local development in kind
#
# Tilt builds every service from the local checkout, loads the resulting
# images into the active kind cluster, and renders the production Helm chart
# with development-only values.

context = k8s_context()
if not context.startswith('kind-'):
    fail('KubeChan development requires a kind context; current context is %s' % context)

allow_k8s_contexts(context)

local_resource(
    'kubechan-namespace',
    cmd='kubectl create namespace kubechan --dry-run=client -o yaml | kubectl apply -f -',
    labels=['setup'],
)

# Go services share the repository-level go.mod and api package.
docker_build(
    'kubechan/backend-api',
    '.',
    dockerfile='services/backend-api/Dockerfile',
    only=['go.mod', 'go.sum', 'api/', 'services/backend-api/'],
)

docker_build(
    'kubechan/cluster-watcher',
    '.',
    dockerfile='services/cluster-watcher/Dockerfile',
    only=['go.mod', 'go.sum', 'api/', 'services/cluster-watcher/'],
)

docker_build(
    'kubechan/diagnostics-worker',
    '.',
    dockerfile='services/diagnostics-worker/Dockerfile',
    only=['go.mod', 'go.sum', 'api/', 'services/diagnostics-worker/'],
)

docker_build(
    'kubechan/llm-gateway',
    'services/llm-gateway',
    only=['Dockerfile', 'pyproject.toml', 'uv.lock', 'main.py', 'app/', 'tests/'],
)

docker_build(
    'kubechan/frontend-ui',
    'services/frontend-ui',
    only=[
        'Dockerfile',
        'package.json',
        'package-lock.json',
        'tsconfig.json',
        'vite.config.ts',
        'index.html',
        'nginx.conf',
        'public/',
        'src/',
    ],
)

# `helm()` renders manifests instead of creating a Helm release. This lets
# Tilt own the lifecycle while exercising the same chart shipped to users.
k8s_yaml(helm(
    'helm/kubechan',
    name='kubechan',
    namespace='kubechan',
    values=['helm/kubechan/values-dev.yaml'],
))

for service in [
    'kubechan-backend-api',
    'kubechan-cluster-watcher',
    'kubechan-diagnostics-worker',
    'kubechan-frontend-ui',
    'kubechan-llm-gateway',
]:
    k8s_resource(service, resource_deps=['kubechan-namespace'])

# kubechan.local is served by ingress-nginx. Keep a port-forward as a direct
# fallback for debugging the UI independently of ingress and local DNS.
k8s_resource('kubechan-frontend-ui', port_forwards=['8080:8080'])

