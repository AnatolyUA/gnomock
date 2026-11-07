# Build from the repository root so the go.work workspace uses the local
# gnomock module: docker build -f cmd/cleaner/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

WORKDIR /gnomock/
ADD go.work .
ADD go.work.sum .
ADD go.mod .
ADD go.sum .
ADD cmd/cleaner/go.mod cmd/cleaner/
ADD cmd/cleaner/go.sum cmd/cleaner/
RUN go mod download -x
RUN go mod verify
ADD . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -a -installsuffix cgo -ldflags '-extldflags "-static"' -o /gnomock-cleaner ./cmd/cleaner

FROM scratch

COPY --from=builder /gnomock-cleaner /gnomock-cleaner
ENTRYPOINT ["/gnomock-cleaner"]
