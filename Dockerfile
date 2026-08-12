# O manager é compilado aqui, a partir do fonte em evolution-go-manager/.
# O upstream `main` só publica o bundle pronto em manager/dist; com o fonte no
# repositório, a tela do Chatwoot faz parte do manager em vez de ser uma página
# solta ao lado dele.
FROM node:22-alpine AS manager

WORKDIR /manager

# package.json e lockfile primeiro: o npm ci só refaz quando a dependência muda,
# e não a cada alteração de componente.
COPY evolution-go-manager/package.json evolution-go-manager/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY evolution-go-manager/ ./
RUN npm run build

FROM golang:1.25.0-alpine AS build

RUN apk update && apk add --no-cache git build-base libjpeg-turbo-dev libwebp-dev

WORKDIR /build

# Copiar apenas arquivos de dependências primeiro para cachear o download
COPY go.mod go.sum ./

# whatsmeow agora vem do proxy oficial (go.mau.fi/whatsmeow, sem replace local) —
# não há mais submódulo whatsmeow-lib para copiar.
RUN go mod download

# Copiar o restante do código
COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=1 go build -ldflags "-X main.version=${VERSION}" -o server ./cmd/evolution-go

FROM alpine:3.19.1 AS final

# poppler-utils provides pdftoppm, used to rasterize PDF page 1 for /send/media document thumbnails
RUN apk update && apk add --no-cache tzdata ffmpeg libjpeg-turbo libwebp poppler-utils

WORKDIR /app

COPY --from=build /build/server .
COPY --from=manager /manager/dist ./manager/dist
COPY --from=build /build/VERSION ./VERSION

ENV TZ=America/Sao_Paulo

ENTRYPOINT ["/app/server"]
