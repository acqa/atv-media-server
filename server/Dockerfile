FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./

FROM alpine:3.19
RUN apk add --no-cache ffmpeg ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/server /usr/local/bin/server
EXPOSE 80 443
ENTRYPOINT ["/usr/local/bin/server"]
