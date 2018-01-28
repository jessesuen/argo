#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

PROJECT_ROOT=$(cd $(dirname "$0")/.. ; pwd)
CODEGEN_PKG=${PROJECT_ROOT}/vendor/k8s.io/code-generator
VERSION="v1alpha1"

go run ${CODEGEN_PKG}/cmd/openapi-gen/main.go \
  -h ${PROJECT_ROOT}/hack/custom-boilerplate.go.txt \
  -i github.com/argoproj/argo/pkg/apis/workflow/${VERSION} \
  -p github.com/argoproj/argo/pkg/apis/workflow/${VERSION}

