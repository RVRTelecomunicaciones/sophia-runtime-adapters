# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY ops/chaos/receiver ./ops/chaos/receiver
RUN cd ops/chaos/receiver && go build -trimpath -ldflags='-s -w' -o /out/receiver-stub .

FROM scratch
COPY --from=build /out/receiver-stub /receiver-stub
EXPOSE 8088
ENTRYPOINT ["/receiver-stub", "--addr=:8088"]
