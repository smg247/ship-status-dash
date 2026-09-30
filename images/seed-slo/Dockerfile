FROM registry.access.redhat.com/ubi9/ubi:latest AS builder

WORKDIR /go/src/ship-status-dash

ENV PATH="/go/bin:${PATH}"
ENV GOPATH="/go"

COPY . .

RUN dnf install -y go make && \
    go build -mod=vendor -o seed-slo ./cmd/seed-slo

FROM registry.access.redhat.com/ubi9/ubi:latest AS base

RUN mkdir -p /app

COPY --from=builder /go/src/ship-status-dash/seed-slo /app/seed-slo

WORKDIR /app

LABEL io.k8s.display-name="SHIP Status SLO Seed" \
      io.openshift.tags="seed,ship-status"

CMD ["./seed-slo"]
