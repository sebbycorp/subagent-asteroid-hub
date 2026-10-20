IMAGE ?= substrate-asteroids:dev
KAGENT_API ?= http://host.docker.internal:8083

.PHONY: test image pf run dev

test:
	go test -race ./...
	node --test web/*.test.mjs

image:
	docker build -t $(IMAGE) .

# The kagent API, reachable from the container via host.docker.internal.
pf:
	kubectl port-forward -n kagent svc/kagent-controller 8083:8083

# Published on 127.0.0.1 only: the control API can spend your model budget.
run:
	docker run --rm --name substrate-asteroids -p 127.0.0.1:7790:7790 -e KAGENT_API=$(KAGENT_API) $(IMAGE)

dev:
	KAGENT_API=http://localhost:8083 go run ./cmd/asteroids
