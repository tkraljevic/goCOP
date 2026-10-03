# goCOP u spremniku:
#
#   docker build -t gocop .
#
# Sliku za svako izdanje gradi GitHub (.github/workflows/slika.yml) i
# objavljuje kao ghcr.io/tkraljevic/gocop. Sve sučelje (predlošci, CSS, JS)
# ugrađeno je u binarnu datoteku, pa slika nosi samo nju.
#
# Dvije mape preživljavaju ponovno pokretanje:
#   /data    baza, ključ čvora, ključ mreže, postavke, baze prognoza i
#            oborina — malo i često pisano, na SSD (na Unraidu appdata)
#   /arhiva  arhiva vodostaja (vodostaji.db, oko 10 GB), izvorno stablo,
#            skenovi i .cop paketi — veliko, na HDD. SQLite ne smije na
#            mrežnu ni FUSE mapu: na Unraidu /mnt/diskN/…, ne /mnt/user/…

FROM golang:1.27-alpine AS gradnja
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
# Verzija je u kodu (cmd/gocop/main.go, verzijaPrograma). .git ne ulazi u
# kontekst, pa oznaku commita u podnožje daje VERSION, koji GitHub zadaje
# pri gradnji izdanja: docker build --build-arg VERSION="0.0.7-alfa (abc1234)"
ARG VERSION=
# Bez C-a: modernc.org/sqlite je čisti Go, pa je binarna datoteka samostalna
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w ${VERSION:+-X 'main.version=${VERSION}'}" \
    -o /gocop ./cmd/gocop

FROM alpine:3.21
# ca-certificates za preuzimanje vodostaja, tzdata za hrvatsko vrijeme
RUN apk add --no-cache ca-certificates tzdata && \
    mkdir -p /data /arhiva && chown 99:100 /data /arhiva
ENV TZ=Europe/Zagreb

COPY --from=gradnja /gocop /usr/local/bin/gocop

# Unraid: ikona (valovi) i poveznica na web sučelje u popisu spremnika
LABEL net.unraid.docker.icon="https://raw.githubusercontent.com/tkraljevic/goCOP/master/web/static/img/gocop-256.png" \
      net.unraid.docker.webui="http://[IP]:[PORT:8080]/"

VOLUME ["/data", "/arhiva"]
WORKDIR /data
# Unraidov korisnik nobody:users, da appdata i dijeljene mape ostanu čitljive
USER 99:100

# Web 8080; razmjena 4710, uparivanje 4711 (samo dok traje), pronalaženje
# 4712/udp (samo lokalna mreža)
EXPOSE 8080 4710 4711 4712/udp

ENTRYPOINT ["gocop", "-config", "/data/gocop.toml", "-db", "/data/gocop.db", "-addr", ":8080", \
    "-arhiva", "/arhiva/vodostaji.db", "-podaci", "/arhiva/vodostaji", \
    "-skenovi", "/arhiva/skenovi", "-pakete", "/arhiva/pakete"]
