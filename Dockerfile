FROM golang:1.13 AS builder

WORKDIR /go/src/app
COPY . .

RUN go get -d -v ./... \
	&& CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app .

FROM  alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY --from=builder /go/src/app/app .
CMD ["/app"]
