FROM golang:1.24 AS builder
WORKDIR /app
COPY . .
RUN go build -o kube-threadsched cmd/kube-threadsched/main.go

FROM gcr.io/distroless/base
COPY --from=builder /app/kube-threadsched /kube-threadsched
ENTRYPOINT ["/kube-threadsched"]

