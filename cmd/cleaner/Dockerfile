FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

WORKDIR /cleaner/
ADD go.mod .
ADD go.sum .
RUN go mod download -x
RUN go mod verify
ADD . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -a -installsuffix cgo -ldflags '-extldflags "-static"' -o /gnomock-cleaner .

FROM scratch

COPY --from=builder /gnomock-cleaner /gnomock-cleaner
ENTRYPOINT ["/gnomock-cleaner"]
