# Build from vendor/, so the image needs no module proxy and no network.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath \
        -ldflags="-s -w" -o /out/xsmb-discord-bot ./cmd/xsmb-discord-bot

FROM alpine:3.20
# ca-certificates is needed for HTTPS to xoso.com.vn. tzdata is not: the
# binary embeds the IANA database via time/tzdata.
RUN apk add --no-cache ca-certificates \
 && adduser -D -u 10001 -H app
USER 10001:10001
COPY --from=build /out/xsmb-discord-bot /usr/local/bin/xsmb-discord-bot
ENTRYPOINT ["xsmb-discord-bot"]
