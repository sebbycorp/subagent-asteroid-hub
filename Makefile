IMAGE ?= substrate-asteroids:dev
KAGENT_API ?= http://host.docker.internal:8083
# Chats to agents enter through agentgateway (see `make agentgateway`).
KAGENT_GATEWAY ?= http://host.docker.internal:8090
AGW_VERSION ?= v2026.10.0

.PHONY: test image pf run dev agentgateway relay

test:
	go test -race ./...
	node --test web/*.test.mjs

image:
	docker build -t $(IMAGE) .

# The kagent API (8083) and agentgateway (8090), reachable from the container
# via host.docker.internal.
pf:
	kubectl port-forward -n kagent svc/kagent-controller 8083:8083 & \
	kubectl port-forward -n agentgateway-system svc/agentgateway-proxy 8090:80 & \
	wait

# Solo Enterprise for agentgateway in front of every agent: model (your Qwen
# on Ollama, plus OpenAI held by the gateway), MCP tools, and chats in.
# Needs AGENTGATEWAY_LICENSE_KEY; OPENAI_API_KEY is stored only in the gateway.
agentgateway: relay
	kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.0/standard-install.yaml
	helm upgrade -i --create-namespace -n agentgateway-system --version $(AGW_VERSION) enterprise-agentgateway-crds \
	  oci://us-docker.pkg.dev/solo-public/enterprise-agentgateway/charts/enterprise-agentgateway-crds
	helm upgrade -i -n agentgateway-system --version $(AGW_VERSION) enterprise-agentgateway \
	  oci://us-docker.pkg.dev/solo-public/enterprise-agentgateway/charts/enterprise-agentgateway \
	  --set-string licensing.licenseKey="$$AGENTGATEWAY_LICENSE_KEY" --wait
	kubectl create secret generic openai-secret -n agentgateway-system \
	  --from-literal=Authorization="$$OPENAI_API_KEY" --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f deploy/agentgateway/

# Lets the cluster reach Ollama on this Mac without exposing it to the LAN.
relay:
	docker rm -f ollama-relay >/dev/null 2>&1 || true
	docker run -d --name ollama-relay --network kind --restart unless-stopped \
	  -v "$(CURDIR)/deploy/agentgateway/ollama-relay.conf:/etc/nginx/conf.d/default.conf:ro" nginx:1.27-alpine

# Published on 127.0.0.1, plus LAN_IP if set (e.g. make run LAN_IP=172.16.10.37).
# Anyone who can reach LAN_IP can press T / run the tour, which spends model
# tokens. On the kind network so agentgateway can report requests to the app.
LAN_IP ?=
PUBLISH = -p 127.0.0.1:7790:7790 $(if $(LAN_IP),-p $(LAN_IP):7790:7790)

run:
	docker run --rm --name substrate-asteroids --network kind $(PUBLISH) \
	  -e KAGENT_API=$(KAGENT_API) -e KAGENT_GATEWAY=$(KAGENT_GATEWAY) $(IMAGE)

dev:
	LISTEN_ADDR=127.0.0.1:7790 KAGENT_API=http://localhost:8083 go run ./cmd/asteroids
