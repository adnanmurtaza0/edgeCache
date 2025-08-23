.PHONY: up down logs ps invalidate demo

up:
	docker compose up --build -d

down:
	docker compose down -v

logs:
	docker compose logs -f --tail=100

ps:
	docker compose ps

# example invalidation (publishes via edge1's /invalidate)
# use to make invalidate PATH=/hello.txt
invalidate:
	curl -s -X POST localhost:8081/invalidate -H "Content-Type: application/json" \
	  -d '{"path":"$(PATH)"}' | jq .

demo:
	bash scripts/demo.sh

