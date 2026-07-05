FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY templates ./templates
COPY static ./static
RUN go build -trimpath -ldflags="-s -w" -o /out/cyberprep .

FROM alpine:3.22

WORKDIR /app
RUN addgroup -S app && adduser -S app -G app && mkdir -p /app/data && chown -R app:app /app
COPY --from=build /out/cyberprep /app/cyberprep
COPY templates ./templates
COPY static ./static

ENV PORT=8080
EXPOSE 8080
USER app
CMD ["/app/cyberprep"]
