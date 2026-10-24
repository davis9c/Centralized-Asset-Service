FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cas ./cmd/cas

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && addgroup -S cas && adduser -S -G cas cas
WORKDIR /app
COPY --from=build /out/cas /app/cas
RUN mkdir -p /data && chown -R cas:cas /data /app
USER cas
ENV CAS_STORAGE_DIR=/data
EXPOSE 8080
ENTRYPOINT ["/app/cas"]
