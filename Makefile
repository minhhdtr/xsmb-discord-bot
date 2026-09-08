COMPOSE ?= docker compose
CORE    ?= services/core
DATE    ?=
FROM    ?=
RATE    ?=
JOBS    ?=

.PHONY: help up down logs logs-core logs-bot restart restart-core restart-bot build test race pgtest fetch gold backfill psql stats tidy fmt

help:
	@grep -E '^[a-z-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

up:      ## Start PostgreSQL, core and the Discord client
	$(COMPOSE) up -d --build

down:    ## Stop everything (the data volume survives)
	$(COMPOSE) down

logs:    ## Follow both services
	$(COMPOSE) logs -f core bot

logs-core: ## Follow core only
	$(COMPOSE) logs -f core

logs-bot: ## Follow the Discord client only
	$(COMPOSE) logs -f bot

restart: ## Rebuild and restart everything
	$(COMPOSE) up -d --build

restart-core: ## Rebuild and restart core only. This is what Go changes need.
	$(COMPOSE) up -d --build core

restart-bot: ## Rebuild and restart the Discord client only
	$(COMPOSE) up -d --build bot

build:   ## Build the core binary locally
	cd $(CORE) && go build -mod=vendor -o ../../bin/core ./cmd/core

test:    ## Run the core test suite
	cd $(CORE) && go test -mod=vendor ./...

race:    ## Run the core test suite under the race detector
	cd $(CORE) && go test -mod=vendor -race ./...

pgtest:  ## Run the storage tests against a real PostgreSQL. Uses xsmb_test, never the real database.
	@$(COMPOSE) up -d db
	@$(COMPOSE) exec -T db sh -c 'psql -U "$${POSTGRES_USER:-xsmb}" -d postgres -tc "SELECT 1 FROM pg_database WHERE datname = '"'"'xsmb_test'"'"'" | grep -q 1 || createdb -U "$${POSTGRES_USER:-xsmb}" xsmb_test'
	$(COMPOSE) --profile tools run --rm pgtest

fetch:   ## Crawl one day and print it. Needs no token, no database. make fetch DATE=14/08/2026
	cd $(CORE) && go run -mod=vendor ./cmd/core fetch $(DATE)

gold:    ## Print the current gold board. Needs no token, no database
	cd $(CORE) && go run -mod=vendor ./cmd/core gold

backfill: ## Fill the archive, then exit. make backfill [FROM=01/10/2005] [JOBS=16] [RATE=200ms]
	$(COMPOSE) run --rm backfill $(if $(FROM),--from $(FROM)) $(if $(JOBS),--concurrency $(JOBS)) $(if $(RATE),--rate $(RATE))

psql:    ## Open psql inside the db container
	$(COMPOSE) exec db psql -U $${POSTGRES_USER:-xsmb} -d $${POSTGRES_DB:-xsmb}

stats:   ## Count what the archive holds
	$(COMPOSE) exec db psql -U $${POSTGRES_USER:-xsmb} -d $${POSTGRES_DB:-xsmb} \
		-c "SELECT count(*) AS draws, min(draw_date), max(draw_date) FROM draws;" \
		-c "SELECT channel_id, guild_id FROM subscriptions;"

tidy:    ## Drop vendor/ and resolve modules from the proxy instead
	cd $(CORE) && rm -rf vendor && go mod tidy

fmt:     ## Format and vet
	cd $(CORE) && gofmt -w . && go vet ./...
