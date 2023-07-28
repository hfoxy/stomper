FROM golang:1.20-alpine AS builder

WORKDIR /build

COPY . ./

RUN go mod download

WORKDIR /build/server
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /go/delivery/stomper

FROM scratch

ENV SERVER_PORT=8448

WORKDIR /app
COPY --from=builder /go/delivery/stomper ./

EXPOSE 8080
CMD [ "/app/stomper" ]
