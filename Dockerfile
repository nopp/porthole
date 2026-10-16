FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/porthole ./cmd/porthole

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/porthole /porthole
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/porthole", "serve", "--addr", ":8080"]
