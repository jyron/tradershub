FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -ldflags="-w -s" -o /out/bottrade-mcp .

FROM debian:bookworm-slim

WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/bottrade-mcp /app/bottrade-mcp
ENV BOTTRADE_MCP_TRANSPORT=http
ENV BOTTRADE_API_BASE=https://bot-trade.org
CMD ["/app/bottrade-mcp"]
