# Build context must be the repo root:
#   docker buildx build --platform linux/amd64 \
#     -f deployments/installer/Dockerfile \
#     -t ghcr.io/wso2/ae-installer:0.0.0-dev --push .
FROM dtzar/helm-kubectl:latest

RUN apk add --no-cache bash curl openssl jq

# Embed all three Helm charts so the installer is self-contained (no registry needed at install time)
COPY deployments/helm-charts/wso2-agentic-engineer-bundle /charts/wso2-agentic-engineer-bundle
COPY deployments/helm-charts/wso2-ae-platform             /charts/wso2-ae-platform
COPY deployments/helm-charts/wso2-ae-oc-extensions        /charts/wso2-ae-oc-extensions

# Pre-vendor subchart dependencies so helm upgrade --install works offline
RUN helm dependency update /charts/wso2-agentic-engineer-bundle

COPY deployments/installer/scripts/ae /scripts/ae
RUN chmod +x /scripts/ae

CMD ["sleep", "infinity"]
