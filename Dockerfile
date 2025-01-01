FROM golang:1.23 as build-stage
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /server .


FROM gcr.io/distroless/base-debian12 AS build-release-stage
WORKDIR /
COPY --from=build-stage /server /server
EXPOSE 3000
USER nonroot:nonroot
ENTRYPOINT ["/server"]
