# goCOP u spremniku. Gradi se iz nadređene mape, jer go.mod pokazuje na
# susjedni syncnet:
#
#   docker build -f goCOP/Dockerfile -t gocop:alfa .
#
# Sve sučelje (predlošci, CSS, JS) ugrađeno je u binarnu datoteku, pa slika
# nosi samo nju. Na disku ostaje jedino mapa /data: baza, ključ čvora,
# ključ mreže i postavke.

FROM golang:1.26-alpine AS gradnja
WORKDIR /src

# syncnet je zaseban repozitorij na koji go.mod pokazuje kroz replace
COPY syncnet/ /syncnet/
COPY goCOP/go.mod goCOP/go.sum ./
RUN go mod download

COPY goCOP/ ./
ARG VERSION=alfa
# Bez C-a: modernc.org/sqlite je čisti Go, pa je binarna datoteka samostalna
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /gocop ./cmd/gocop

FROM alpine:3.21
# ca-certificates za buduće preuzimanje vodostaja, tzdata za hrvatsko vrijeme
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -u 1000 gocop
ENV TZ=Europe/Zagreb

COPY --from=gradnja /gocop /usr/local/bin/gocop

# Sve što preživi ponovno pokretanje spremnika
VOLUME /data
WORKDIR /data
USER gocop

# Web sučelje; sinkronizacija je na 4710, uparivanje 4711, pronalaženje 4712
EXPOSE 8088 4710 4711 4712

ENTRYPOINT ["gocop", "-config", "/data/gocop.toml", "-db", "/data/gocop.db"]
