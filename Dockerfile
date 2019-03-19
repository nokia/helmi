FROM golang:1.12-alpine as builder

# Install dependencies
RUN apk add --update --no-cache ca-certificates tar wget

# Build helmi
WORKDIR /go/src/github.com/monostream/helmi/

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o helmi .

# Copy helm artefacts
WORKDIR /app/
RUN cp /go/src/github.com/monostream/helmi/helmi .
RUN rm -r /go/src/

# Download helm 2.13.0
RUN wget -nv -O- https://storage.googleapis.com/kubernetes-helm/helm-v2.13.0-linux-amd64.tar.gz | tar --strip-components=1 -zxf -

# Download dumb-init 1.2.1
RUN wget -nv -O /usr/local/bin/dumb-init https://github.com/Yelp/dumb-init/releases/download/v1.2.1/dumb-init_1.2.1_amd64 && chmod 755 /usr/local/bin/dumb-init

FROM alpine:3.9
RUN apk add --update --no-cache ca-certificates

WORKDIR /app/

COPY --from=builder /app/ .
COPY --from=builder /usr/local/bin/dumb-init /usr/local/bin/dumb-init

# Setup environment
ENV PATH "/app:${PATH}"
ENV REPOSITORY_URLS '{"monostream":"http://helm-charts.monocloud.io"}'

RUN addgroup -S helmi && \
    adduser -S -G helmi helmi && \
    chown -R helmi:helmi /app

USER helmi

# Initialize helm
RUN helm init --client-only

ENTRYPOINT ["/usr/local/bin/dumb-init", "--"]

CMD ["helmi"]
