FROM golang:1.23 as build-stage
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /server .


FROM alpine:3.21 AS build-release-stage
WORKDIR /

# Needed for health check
RUN apk add --no-cache curl

COPY --from=build-stage /server /server
EXPOSE 3000
ENTRYPOINT ["/server"]
