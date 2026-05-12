FROM golang:1.22-bookworm AS build
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev libsqlite3-dev && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -tags "sqlite_omit_load_extension" -o /out/wh-cli ./cmd/wh-cli

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libsqlite3-0 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/wh-cli /usr/local/bin/wh-cli
ENTRYPOINT ["wh-cli"]
