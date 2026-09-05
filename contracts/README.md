# contracts

`openapi.yaml` is the agreement between `core` and `bot`. It lives here rather
than inside either service because neither owns it: a change lands in this
folder, and the diff makes plain that both sides are affected.

Both ends **generate** from it. That matters more than it looks. The old bot
shared Go types with core, so a renamed field broke the build. Once the bot is
TypeScript that safety is gone, and generated code is what buys it back — a
hand-written interface drifts silently, and six months later the YAML is
decoration.

    # Go server stubs
    cd services/core && go generate ./internal/httpapi

    # TypeScript client
    cd services/bot && npm run gen:api

Neither is wired up yet; the file arrives with step 2.
