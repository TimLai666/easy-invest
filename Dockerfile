FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG CMD=server
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/easy-invest ./cmd/${CMD}

FROM alpine:3.22
# postgresql16-client 提供 pg_dump / pg_restore，供 worker 每日備份與還原使用。
RUN apk add --no-cache ca-certificates tzdata postgresql16-client
WORKDIR /app
COPY --from=build /out/easy-invest /usr/local/bin/easy-invest
EXPOSE 8080
ENTRYPOINT ["easy-invest"]

