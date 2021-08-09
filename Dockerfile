FROM golang:1.16-alpine AS builder

# copy source files
WORKDIR /src
RUN apk add --no-cache git
COPY source/ .

# get deps and build
RUN go get -d -v
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/prometheus-ecs-sd .

# runtime image
FROM scratch
COPY --from=builder /go/bin/prometheus-ecs-sd /go/bin/prometheus-ecs-sd
ENTRYPOINT ["/go/bin/prometheus-ecs-sd"]
