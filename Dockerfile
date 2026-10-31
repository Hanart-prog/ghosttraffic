# GhostTraffic CLI and dashboard, for hosts without Go or Node.
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

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
COPY --from=web /web/dist /web
USER 65534:65534
ENTRYPOINT ["/ghosttraffic"]
