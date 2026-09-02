# The self-hoster's image: one static binary, no Node, no runtime toolchain.
# The marketing site is deliberately not here — it is deployed separately and
# never ships to self-hosters.

FROM golang:1.24-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/phenk ./cmd/phenk

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 phenk \
 && mkdir -p /var/lib/phenk/blobs \
 && chown -R phenk /var/lib/phenk

COPY --from=build /out/phenk /usr/local/bin/phenk

USER phenk
ENV PHENK_BLOB_DIR=/var/lib/phenk/blobs
EXPOSE 25 8080
ENTRYPOINT ["/usr/local/bin/phenk"]
CMD ["all"]
