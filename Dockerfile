FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/lab-server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/lab-server /lab-server
ENV LISTEN_ADDR=0.0.0.0:10000
ENV DATA_DIR=/tmp/data
EXPOSE 10000
USER nonroot:nonroot
ENTRYPOINT ["/lab-server"]
