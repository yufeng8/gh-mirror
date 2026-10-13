# 交叉编译；本地用 --build-arg GOARCH=amd64|arm64 指定（deploy.sh / install.sh 会传入）。
# 不传时由 buildx + QEMU 按目标平台原生编译，用于 GitHub Actions 多架构构建。
FROM golang:1.22 AS build
ARG GOARCH=
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux ${GOARCH:+GOARCH=$GOARCH} go build -o /out/github-mirror .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/github-mirror /github-mirror
EXPOSE 8080
ENTRYPOINT ["/github-mirror"]
