FROM golang:1.17 as builder
WORKDIR /gowork
ENV GOPROXY https://proxy.golang.com.cn,direct
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -a -ldflags '-s' -o app main.go


FROM alpine AS prod
WORKDIR /gowork
RUN set -eux && sed -i 's/dl-cdn.alpinelinux.org/mirrors.ustc.edu.cn/g' /etc/apk/repositories &&\
  apk add -U tzdata && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && apk del tzdata &&\
  rm -rf /tmp/ /var/cache/apk/
COPY --from=builder /gowork/app /gowork/app

CMD ["/gowork/app"]