FROM golang:1.11-alpine as builder

# Install build dependencies such as git and glide.
RUN apk add --no-cache git gcc musl-dev

RUN apk add --no-cache --update alpine-sdk \
    git \
    make \
    bash \
    gcc
    
COPY cmd cmd
RUN cd cmd && go build

# Start a new image
FROM alpine as final

COPY --from=builder /go/cmd/lndmon /bin/
COPY "tls.cert" .
COPY "admin.macaroon" .

# Add bash and ca-certs, for quality of life and SSL-related reasons.
RUN apk --no-cache add \
    bash \
    busybox \
    iputils \
    && chmod +x /bin/lndmon

CMD lndmon