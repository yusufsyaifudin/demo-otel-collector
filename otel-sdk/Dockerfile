FROM docker.io/library/golang:1.23-alpine3.20 AS builder

WORKDIR /otel-sdk

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN mkdir -p /app
RUN CGO_ENABLED=0 go build -o /app/app.bin main.go

FROM gcr.io/distroless/static-debian12:6755e21ccd99ddead6edc8106ba03888cbeed41a
LABEL MAINTAINER="Yusuf Syaifudin <yusuf.syaifudin@gmail.com>"

COPY --from=builder /app/app.bin /
CMD ["/app.bin"]
