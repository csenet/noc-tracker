# syntax=docker/dockerfile:1.7

# ----- build -----
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO off so we get a fully static binary that runs on alpine.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/noc-tracker .

# ----- runtime -----
FROM alpine:3.20
# expect + openssh-client are required by the IAP source (see iap/client.go),
# ca-certificates for the Aruba Instant On HTTPS calls, tzdata so log
# timestamps render in the user's TZ when TZ=... is passed.
RUN apk add --no-cache expect openssh-client ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/noc-tracker /usr/local/bin/noc-tracker
ENV NOC_LISTEN=:8080 \
    NOC_STORE_PATH=/data/registrations.json \
    NOC_AP_POSITIONS_PATH=/data/ap-positions.json \
    NOC_FLOORPLAN_DIR=/data
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/noc-tracker"]
