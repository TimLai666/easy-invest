FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG CMD=server
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/easy-invest ./cmd/${CMD}

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/easy-invest /usr/local/bin/easy-invest
EXPOSE 8080
ENTRYPOINT ["easy-invest"]

