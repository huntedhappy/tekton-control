# ----------------------------------------
# 컨트롤러 개발용 Makefile
# ----------------------------------------
export GOPATH := $(shell go env GOPATH)

# --- 변수 정의 ---
CONTROLLER_GEN_VERSION ?= v0.15.0
REPO                   ?= harbor-infra.huntedhappy.kro.kr/library/tekton-controller
IMAGE_TAG              ?= $(shell date +%Y%m%d%H%M)
IMAGE                  := $(REPO):$(IMAGE_TAG)
DEPLOY_YAML            ?= deploy/deployment.yaml
DEPLOY_NAMESPACE       ?= tekton-operator
DOCKERFILE             := Dockerfile.gen
CONTROLLER_GEN_BIN     := $(shell go env GOPATH)/bin/controller-gen

# --- 바이너리 확인 ---
.PHONY: ensure-controller-gen
ensure-controller-gen:
	@echo "🔧 controller-gen 확인/설치 ($(CONTROLLER_GEN_VERSION))..."
	@if ! [ -x "$(CONTROLLER_GEN_BIN)" ]; then \
		echo "Installing controller-gen to $(CONTROLLER_GEN_BIN)..."; \
		GO111MODULE=on GOBIN=$$(go env GOPATH)/bin go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION); \
	else \
		echo "✅ controller-gen found: $(CONTROLLER_GEN_BIN)"; \
	fi

# --- 코드 포맷팅 ---
.PHONY: fmt
fmt:
	@echo "🧹 Running gofmt..."
	gofmt -s -w .

# --- 코드 품질 ---
.PHONY: vet
vet:
	@echo "🔎 Running go vet..."
	go vet ./...

# --- Go 모듈 캐시 정리 ---
.PHONY: prompt-clean-modcache
prompt-clean-modcache:
	@read -p "🛉 Do you want to run 'go clean -modcache'? [y/N(Default)]: " answer; \
	case $$answer in \
		y|Y) \
			echo "🛉 Cleaning Go module cache..."; \
			go clean -modcache; \
			echo "✅ Done."; \
			;; \
		*) \
			echo "🚫 Skipped 'go clean -modcache'."; \
			;; \
	esac

# --- 의존성 관리 ---
.PHONY: deps-reset
deps-reset:
	@echo "💣 Deleting mod files..."
	@rm -f go.mod go.sum
	@echo "🔄 Initializing new go.mod..."
	@go mod init tekton-controller
	@echo "⬇️  Fetching required core dependencies..."
	@go get github.com/tektoncd/pipeline@v0.61.0
	@go get sigs.k8s.io/controller-runtime@v0.21.0
	@go get k8s.io/client-go@v0.30.2
	@echo "✨ Running go mod tidy..."
	@go mod tidy
	@echo "✅ Dependency reset complete."
	@go list -m github.com/tektoncd/pipeline

# --- CRD 생성 ---
.PHONY: manifests
manifests: ensure-controller-gen
	@echo "📜 Generating CRD YAML files..."
	@mkdir -p config/crd/bases
	@$(CONTROLLER_GEN_BIN) crd:crdVersions=v1 \
		paths="./api/..." \
		output:crd:dir=config/crd/bases \
		object:headerFile="hack/boilerplate.go.txt"

# --- 빌드 & 테스트 ---
.PHONY: mod-tidy build test test-controllers ci envtest
mod-tidy:
	go mod tidy

build: fmt ensure-controller-gen manifests mod-tidy
	go build ./...

test:
	go clean -testcache
	go test ./... -v

test-controllers:
	go clean -testcache
	go test ./controllers/... -v

ci: fmt build test vet
	@echo "✅ Build, Test & Vet passed"

envtest:
	GO111MODULE=on go test ./... -tags=integration -v

# --- 릴리즈: 이미지 빌드·푸시·배포 ---
.PHONY: release docker-build docker-push kubernetes-deploy clean

# 메인 릴리즈 타겟
release: prompt-clean-modcache ci manifests docker-build docker-push kubernetes-deploy
	@echo "✅ Release complete: image=$(IMAGE)"

# Dockerfile 생성 및 이미지 빌드
docker-build:
	@echo "📦 Generating Dockerfile..."
	@printf '%s\n' \
		'# Build stage' \
		'FROM golang:1.24 AS builder' \
		'' \
		'WORKDIR /workspace' \
		'COPY go.mod go.sum ./' \
		'RUN go mod download' \
		'' \
		'COPY . .' \
		'RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go' \
		'' \
		'# Runtime stage' \
		'FROM debian:bullseye-slim' \
		'' \
		'RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*' \
		'' \
		'WORKDIR /' \
		'COPY --from=builder /workspace/manager .' \
		'' \
		'USER 65532:65532' \
		'' \
		'ENTRYPOINT ["/manager"]' \
		> $(DOCKERFILE)
	@echo "🎯 Building image $(IMAGE) from $(DOCKERFILE)..."
	docker build --no-cache -t $(IMAGE) -f $(DOCKERFILE) .

# Docker 이미지 푸시
docker-push:
	@echo "🚀 Pushing image $(IMAGE)..."
	docker push $(IMAGE)

# 쿠버네티스 배포
DEPLOY_NAMESPACE ?= tekton-operator
kubernetes-deploy:
	@echo "👀 Checking for existing Deployment 'tekton-controller' in namespace '$(DEPLOY_NAMESPACE)'..."
	@if kubectl get deployment tekton-controller -n $(DEPLOY_NAMESPACE) >/dev/null 2>&1; then \
		echo "🛠️  Deployment exists, updating image to $(IMAGE)..."; \
		kubectl -n $(DEPLOY_NAMESPACE) set image deployment/tekton-controller tekton-controller=$(IMAGE); \
	else \
		if [ -f "$(DEPLOY_YAML)" ]; then \
			echo "🆕 Deployment not found, applying temporary manifest with new image tag..."; \
			cat $(DEPLOY_YAML) | sed 's|image: .*|image: $(IMAGE)|g' | kubectl apply -f -; \
		else \
			echo "‼️ Error: Deployment manifest '$(DEPLOY_YAML)' not found."; \
			exit 1; \
		fi \
	fi

# 빌드 결과물 정리
clean:
	@echo "🪟 Cleaning build artifacts..."
	@rm -f manager $(DOCKERFILE)
	@go clean -testcache
	@echo "🪟 Cleaning Docker image: $(IMAGE)..."
	@docker rmi $(IMAGE) 2>/dev/null || true
