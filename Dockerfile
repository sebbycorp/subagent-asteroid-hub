# syntax=docker/dockerfile:1
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /asteroids ./cmd/asteroids

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /asteroids /asteroids
EXPOSE 7790
ENTRYPOINT ["/asteroids"]
