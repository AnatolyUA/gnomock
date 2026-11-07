FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

WORKDIR /gnomock/
ADD go.mod .
ADD go.sum .
RUN go mod download -x
RUN go mod verify
ADD . .
# The server is built from the root module alone; go.work only adds cmd/cleaner.
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOWORK=off go build -trimpath -a -installsuffix cgo -ldflags '-extldflags "-static"' -o /gnomockd ./cmd/server

FROM scratch

COPY --from=builder /gnomockd /gnomockd
ENV GNOMOCK_ENV=gnomockd
ENTRYPOINT ["/gnomockd"]
