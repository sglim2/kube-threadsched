FROM golang:1.18 AS builder
WORKDIR /app
COPY . .
RUN go build -o kube-threadsched cmd/main.go

FROM gcr.io/distroless/base
COPY --from=builder /app/ratio-scheduler /ratio-scheduler
ENTRYPOINT ["/ratio-scheduler"]

