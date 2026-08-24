COMPOSE ?= docker compose
DATE    ?=
FROM    ?=
RATE    ?=
JOBS    ?=

.PHONY: help up down logs restart build test race fetch gold backfill psql stats tidy fmt

help:
	@grep -E '^[a-z-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

up:      ## Start PostgreSQL and the bot
	$(COMPOSE) up -d --build

down:    ## Stop everything (the data volume survives)
	$(COMPOSE) down

logs:    ## Follow the bot's logs
	$(COMPOSE) logs -f bot

restart: ## Rebuild and restart just the bot
	$(COMPOSE) up -d --build bot

build:   ## Build the binary locally
	go build -mod=vendor -o bin/xsmb-discord-bot ./cmd/xsmb-discord-bot

test:    ## Run the test suite
	go test -mod=vendor ./...

race:    ## Run the test suite under the race detector
	go test -mod=vendor -race ./...

fetch:   ## Crawl one day and print it. Needs no token, no database. make fetch DATE=14/08/2026
	go run -mod=vendor ./cmd/xsmb-discord-bot fetch $(DATE)

gold:    ## Print the current gold board. Needs no token, no database
	go run -mod=vendor ./cmd/xsmb-discord-bot gold

backfill: ## Fill the archive, then exit. make backfill [FROM=01/10/2005] [JOBS=16] [RATE=200ms]
	$(COMPOSE) run --rm backfill $(if $(FROM),--from $(FROM)) $(if $(JOBS),--concurrency $(JOBS)) $(if $(RATE),--rate $(RATE))

psql:    ## Open psql inside the db container
	$(COMPOSE) exec db psql -U $${POSTGRES_USER:-xsmb} -d $${POSTGRES_DB:-xsmb}

stats:   ## Count what the archive holds
	$(COMPOSE) exec db psql -U $${POSTGRES_USER:-xsmb} -d $${POSTGRES_DB:-xsmb} \
		-c "SELECT count(*) AS draws, min(draw_date), max(draw_date) FROM draws;" \
		-c "SELECT channel_id, guild_id FROM subscriptions;"

tidy:    ## Drop vendor/ and resolve modules from the proxy instead
	rm -rf vendor && go mod tidy

fmt:     ## Format and vet
	gofmt -w . && go vet ./...
