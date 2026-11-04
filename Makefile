.PHONY: generate generate-objects generate-crds dev-check dev-up dev-down dev-clean lint lint-go lint-python test test-go test-python build build-go build-python build-frontend install-tools install-hooks e2e e2e-clean help

# ─── Variables ────────────────────────────────────────────────────────────────

# Pinned controller-gen version. Run 'make install-tools' once to install it.
CONTROLLER_GEN_VERSION := v0.21.0

# Prefer locally installed controller-gen; fall back to 'go run' for CI.
ifeq (, $(shell which controller-gen))
CONTROLLER_GEN := go run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
else
CONTROLLER_GEN := controller-gen
endif

# ─── Code generation ─────────────────────────────────────────────────────────

## generate: Run all code generation (DeepCopy + CRD YAMLs)
generate: generate-objects generate-crds

## generate-objects: Generate DeepCopy methods via controller-gen
generate-objects:
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

## generate-crds: Generate CRD YAMLs via controller-gen
generate-crds:
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=helm/kubechan/crds/

# ─── Local dev ───────────────────────────────────────────────────────────────

## dev-check: Verify that the active Kubernetes context is a reachable kind cluster
dev-check:
	@context=$$(kubectl config current-context); \
	case "$$context" in \
		kind-*) ;; \
		*) echo "error: expected a kind context, got '$$context'"; exit 1 ;; \
	esac; \
	kubectl cluster-info >/dev/null; \
	echo "Using $$context"

## dev-up: Build local images and deploy the chart to the active kind cluster with Tilt
dev-up: dev-check
	tilt up

## dev-down: Stop Tilt
dev-down:
	tilt down

## dev-clean: Remove Tilt resources and the dev namespace (including its PVC/data)
dev-clean: dev-check
	tilt down
	kubectl delete namespace kubechan --ignore-not-found

# ─── Linting ─────────────────────────────────────────────────────────────────

## lint: Run all linters
lint: lint-go lint-python

## lint-go: Run golangci-lint on all Go packages
lint-go:
	golangci-lint run ./...

## lint-python: Run pyright on llm-gateway
lint-python:
	cd services/llm-gateway && uv run pyright .

# ─── Testing ─────────────────────────────────────────────────────────────────

## test: Run all unit tests
test: test-go test-python

## test-go: Run Go unit tests
test-go:
	go test ./...

## test-python: Run Python unit tests for llm-gateway
test-python:
	cd services/llm-gateway && uv run pytest -v

# ─── Build (local verification) ──────────────────────────────────────────────

## build: Build all services locally
build: build-go build-python build-frontend

## build-go: Compile all Go services
build-go:
	go build ./services/cluster-watcher/
	go build ./services/diagnostics-worker/
	go build ./services/backend-api/

## build-python: Synchronize locked llm-gateway dependencies
build-python:
	cd services/llm-gateway && uv sync --frozen --group dev

## build-frontend: Build the frontend-ui static assets
build-frontend:
	cd services/frontend-ui && npm ci && npm run build

# ─── Tools ───────────────────────────────────────────────────────────────────

## install-tools: Install controller-gen to GOPATH/bin
install-tools:
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

## install-hooks: Install git hooks from hack/hooks/ into .git/hooks/
install-hooks:
	@for hook in hack/hooks/*; do \
	  name=$$(basename $$hook); \
	  ln -sf ../../$$hook .git/hooks/$$name; \
	  chmod +x .git/hooks/$$name; \
	  echo "installed .git/hooks/$$name -> ../../$$hook"; \
	done

# ─── E2E ─────────────────────────────────────────────────────────────────────

## e2e: Build, deploy, and wait for all workloads in the active kind cluster
e2e: dev-check
	tilt ci

## e2e-clean: Uninstall kubechan and delete CRDs for a clean slate
e2e-clean:
	helm uninstall kubechan --namespace kubechan --ignore-not-found || true
	kubectl delete crd problemcases.kubechan.io incidents.kubechan.io \
	    diagnosticruns.kubechan.io kubechanstates.kubechan.io \
	    kubechanexclusionrules.kubechan.io \
	    --ignore-not-found || true

# ─── Help ─────────────────────────────────────────────────────────────────────

## help: Show this help message
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
