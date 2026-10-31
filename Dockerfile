# GhostTraffic CLI, for hosts without a Go toolchain.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
COPY pkg pkg
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /ghosttraffic ./cmd/ghosttraffic

FROM scratch
COPY --from=build /ghosttraffic /ghosttraffic
USER 65534:65534
ENTRYPOINT ["/ghosttraffic"]
