FROM golang:alpine as build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -o /bin/edge

FROM alpine
WORKDIR /app
COPY --from=build /bin/edge /app/edge
# assets mounted from host via volume
EXPOSE 8080
ENTRYPOINT ["/app/edge"]