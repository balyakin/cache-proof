FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION:-dev}" -o /cacheproof ./cmd/cacheproof

FROM scratch
WORKDIR /work
COPY --from=build /cacheproof /work/cacheproof
ENTRYPOINT ["/work/cacheproof"]
